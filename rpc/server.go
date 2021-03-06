package rpc

import (
	"fmt"
	"github.com/duanhf2012/origin/log"
	"github.com/duanhf2012/origin/network"
	"math"
	"net"
	"reflect"
	"strings"
)
type RpcProcessorType uint8

const (
	RpcProcessorJson RpcProcessorType = 0
	RpcProcessorPb   RpcProcessorType = 1
)

//var processor IRpcProcessor = &JsonProcessor{}
var arrayProcessor  = []IRpcProcessor{&JsonProcessor{},&PBProcessor{}}
var arrayProcessorLen uint8 = 2
var LittleEndian bool

type Server struct {
	functions       map[interface{}]interface{}
	cmdChannel      chan *Call
	rpcHandleFinder RpcHandleFinder
	rpcServer       *network.TCPServer
}

type RpcAgent struct {
	conn      network.Conn
	rpcServer *Server
	userData  interface{}
}

func AppendProcessor(rpcProcessor IRpcProcessor) {
	arrayProcessor = append(arrayProcessor,rpcProcessor)
	arrayProcessorLen++
}

func GetProcessorType(param interface{}) (RpcProcessorType,IRpcProcessor){
	for i:=uint8(1);i<arrayProcessorLen;i++{
		if arrayProcessor[i].IsParse(param) == true {
			return RpcProcessorType(i),arrayProcessor[i]
		}
	}

	return RpcProcessorJson,arrayProcessor[RpcProcessorJson]
}

func GetProcessor(processorType uint8) IRpcProcessor{
	if processorType>=arrayProcessorLen{
		return nil
	}
	return arrayProcessor[processorType]
}

func (server *Server) Init(rpcHandleFinder RpcHandleFinder) {
	server.cmdChannel = make(chan *Call,100000)
	server.rpcHandleFinder = rpcHandleFinder
	server.rpcServer = &network.TCPServer{}
}

func (server *Server) Start(listenAddr string) {
	 splitAddr := strings.Split(listenAddr,":")
	 if len(splitAddr)!=2{
	 	log.Fatal("listen addr is error :%s",listenAddr)
	 }

	server.rpcServer.Addr = ":"+splitAddr[1]
	server.rpcServer.LenMsgLen = 2 //uint16
	server.rpcServer.MinMsgLen = 2
	server.rpcServer.MaxMsgLen = math.MaxUint16
	server.rpcServer.MaxConnNum = 10000
	server.rpcServer.PendingWriteNum = 2000000
	server.rpcServer.NewAgent = server.NewAgent
	server.rpcServer.LittleEndian = LittleEndian
	server.rpcServer.Start()
}

func (agent *RpcAgent) OnDestroy() {}

func (agent *RpcAgent) WriteResponse(processor IRpcProcessor,serviceMethod string,seq uint64,reply interface{},err RpcError) {
	var mReply []byte
	var rpcError RpcError
	var errM error


	if reply!=nil {
		mReply,errM = processor.Marshal(reply)
		if errM != nil {
			rpcError = ConvertError(errM)
		}
	}

	var rpcResponse RpcResponse
	rpcResponse.RpcResponseData = processor.MakeRpcResponse(seq,rpcError,mReply)
	bytes,errM :=  processor.Marshal(rpcResponse.RpcResponseData)
	defer processor.ReleaseRpcResponse(rpcResponse.RpcResponseData)

	if errM != nil {
		log.Error("service method %s %+v Marshal error:%+v!", serviceMethod,rpcResponse,errM)
		return
	}

	errM = agent.conn.WriteMsg([]byte{uint8(processor.GetProcessorType())},bytes)
	if errM != nil {
		log.Error("Rpc %s return is error:%+v",serviceMethod,errM)
	}
}

func (agent *RpcAgent) Run() {
	for {
		data,err := agent.conn.ReadMsg()
		if err != nil {
			log.Error("read message: %v", err)
			//will close tcpconn
			break
		}

		processor := GetProcessor(uint8(data[0]))
		if processor==nil {
			agent.conn.ReleaseReadMsg(data)
			log.Error("remote rpc  %s data head error:%+v",agent.conn.RemoteAddr(),err)
			return
		}

		//解析head
		req := MakeRpcRequest()
		req.rpcProcessor = processor
		req.RpcRequestData = processor.MakeRpcRequest(0,"",false,nil)
		err = processor.Unmarshal(data[1:],req.RpcRequestData)
		agent.conn.ReleaseReadMsg(data)
		if err != nil {
			log.Error("rpc Unmarshal request is error: %v", err)
			if req.RpcRequestData.GetSeq()>0 {
				rpcError := RpcError(err.Error())
				agent.WriteResponse(processor,req.RpcRequestData.GetServiceMethod(),req.RpcRequestData.GetSeq(),nil,rpcError)
				processor.ReleaseRpcRequest(req.RpcRequestData)
				ReleaseRpcRequest(req)
				continue
			}else{
				//will close tcpconn
				processor.ReleaseRpcRequest(req.RpcRequestData)
				ReleaseRpcRequest(req)
				break
			}
		}

		//交给程序处理
		serviceMethod := strings.Split(req.RpcRequestData.GetServiceMethod(),".")
		if len(serviceMethod)!=2 {
			rpcError := RpcError("rpc request req.ServiceMethod is error")
			agent.WriteResponse(processor,req.RpcRequestData.GetServiceMethod(),req.RpcRequestData.GetSeq(),nil,rpcError)
			processor.ReleaseRpcRequest(req.RpcRequestData)
			ReleaseRpcRequest(req)
			log.Debug("rpc request req.ServiceMethod is error")
			continue
		}

		rpcHandler := agent.rpcServer.rpcHandleFinder.FindRpcHandler(serviceMethod[0])
		if rpcHandler== nil {
			rpcError := RpcError(fmt.Sprintf("service method %s not config!", req.RpcRequestData.GetServiceMethod()))
			agent.WriteResponse(processor,req.RpcRequestData.GetServiceMethod(),req.RpcRequestData.GetSeq(),nil,rpcError)
			processor.ReleaseRpcRequest(req.RpcRequestData)
			ReleaseRpcRequest(req)
			log.Error("service method %s not config!", req.RpcRequestData.GetServiceMethod())
			continue
		}

		if req.RpcRequestData.IsNoReply()==false {
			req.requestHandle = func(Returns interface{},Err RpcError){
				agent.WriteResponse(processor,req.RpcRequestData.GetServiceMethod(),req.RpcRequestData.GetSeq(),Returns,Err)
			}
		}

		err = rpcHandler.PushRequest(req)
		if err != nil {
			rpcError := RpcError(err.Error())

			if req.RpcRequestData.IsNoReply() {
				agent.WriteResponse(processor,req.RpcRequestData.GetServiceMethod(),req.RpcRequestData.GetSeq(),nil,rpcError)
			}

			processor.ReleaseRpcRequest(req.RpcRequestData)
			ReleaseRpcRequest(req)
		}
	}
}

func (agent *RpcAgent) OnClose() {
}

func (agent *RpcAgent) WriteMsg(msg interface{}) {
}

func (agent *RpcAgent) LocalAddr() net.Addr {
	return agent.conn.LocalAddr()
}

func (agent *RpcAgent) RemoteAddr() net.Addr {
	return agent.conn.RemoteAddr()
}

func (agent *RpcAgent)  Close() {
	agent.conn.Close()
}

func (agent *RpcAgent) Destroy() {
	agent.conn.Destroy()
}

func (server *Server) NewAgent(c *network.TCPConn) network.Agent {
	agent := &RpcAgent{conn: c, rpcServer: server}

	return agent
}

func (server *Server) myselfRpcHandlerGo(handlerName string,serviceMethod string, args interface{},reply interface{}) error {
	rpcHandler := server.rpcHandleFinder.FindRpcHandler(handlerName)
	if rpcHandler== nil {
		err := fmt.Errorf("service method %s not config!", serviceMethod)
		log.Error("%s",err.Error())
		return err
	}

	return rpcHandler.CallMethod(serviceMethod,args,reply)
}


func (server *Server) selfNodeRpcHandlerGo(processor IRpcProcessor,client *Client,noReply bool,handlerName string,serviceMethod string, args interface{},reply interface{},inputArgs IRawInputArgs) *Call {
	pCall := MakeCall()
	pCall.Seq = client.generateSeq()

	rpcHandler := server.rpcHandleFinder.FindRpcHandler(handlerName)
	if rpcHandler== nil {
		if inputArgs!= nil {
			inputArgs.DoGc()
		}
		pCall.Err = fmt.Errorf("service method %s not config!", serviceMethod)
		log.Error("%s",pCall.Err.Error())
		pCall.done <- pCall
		return pCall
	}
	req :=  MakeRpcRequest()

	req.bLocalRequest = true
	req.localParam = args
	req.localReply = reply
	req.inputArgs = inputArgs
	if processor == nil {
		_,processor = GetProcessorType(args)
	}

	req.RpcRequestData = processor.MakeRpcRequest(0, serviceMethod,noReply,nil)
	req.rpcProcessor = processor
	if noReply == false {
		client.AddPending(pCall)
		req.requestHandle = func(Returns interface{},Err RpcError){
			v := client.RemovePending(pCall.Seq)
			if v == nil {
				log.Error("rpcClient cannot find seq %d in pending",pCall.Seq)
				ReleaseCall(pCall)
				return
			}
			if len(Err) == 0 {
				pCall.Err = nil
			}else{
				pCall.Err = Err
			}
			pCall.done <- pCall
		}
	}

	err := rpcHandler.PushRequest(req)
	if err != nil {
		processor.ReleaseRpcRequest(req.RpcRequestData)
		ReleaseRpcRequest(req)
		pCall.Err = err
		pCall.done <- pCall
	}

	return pCall
}

func (server *Server) selfNodeRpcHandlerAsyncGo(client *Client,callerRpcHandler IRpcHandler,noReply bool,handlerName string,serviceMethod string,args interface{},reply interface{},callback reflect.Value) error {
	pCall := MakeCall()
	pCall.Seq = client.generateSeq()
	pCall.rpcHandler = callerRpcHandler
	pCall.callback = &callback
	pCall.Reply = reply
	rpcHandler := server.rpcHandleFinder.FindRpcHandler(handlerName)
	if rpcHandler== nil {
		err := fmt.Errorf("service method %s not config!", serviceMethod)
		log.Error("%+v",err)
		ReleaseCall(pCall)
		return err
	}

	req := MakeRpcRequest()
	req.localParam = args
	req.localReply = reply
	req.bLocalRequest = true
	_,processor := GetProcessorType(args)
	req.rpcProcessor =processor
	req.RpcRequestData = processor.MakeRpcRequest(0,serviceMethod,noReply,nil)
	if noReply == false {
		client.AddPending(pCall)
		req.requestHandle = func(Returns interface{},Err RpcError){
			v := client.RemovePending(pCall.Seq)
			if v == nil {
				log.Error("rpcClient cannot find seq %d in pending",pCall.Seq)
				ReleaseCall(pCall)
				processor.ReleaseRpcRequest(req.RpcRequestData)
				ReleaseRpcRequest(req)
				return
			}
			if len(Err) == 0 {
				pCall.Err = nil
			}else{
				pCall.Err = Err
			}

			if Returns!=nil {
				pCall.Reply = Returns
			}
			pCall.rpcHandler.(*RpcHandler).callResponseCallBack <-pCall
		}
	}

	err := rpcHandler.PushRequest(req)
	if err != nil {
		processor.ReleaseRpcRequest(req.RpcRequestData)
		ReleaseRpcRequest(req)

		return err
	}

	return nil
}
