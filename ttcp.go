package ttcp

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	ONLINE int = 1001
	OFFLIEN int = 1002
)

type Server struct {
	timeout int64							//收包超时时间
	onCreate      func(* Client)		//当有客户端连上来触发
	onRecv      func(* Client, *[]byte)		//收到一个完整的包触发 第一个int32必须是包长
	onClose     func(* Client)		//当有客户端关闭
}



type Client struct {
	Conn net.Conn
	timeout int64
	lastRecvTime int64
	chanNotification chan int	//通知心跳检测协程 当前已经退出
	status int
	lckStatus sync.Mutex
	data interface{}

	onCreate      func(* Client)		//连接成功
	onRecv      func(* Client, *[]byte)		//收到一个完整的包触发 第一个int32必须是包长
	onClose     func(* Client)		//当有客户端关闭
}

func log(fmtStr string, v ...interface{})  {
	fmt.Printf(fmtStr, v...)
}

func (this *Server)serverClientHeart(client *Client) {


	for {
		client.lckStatus.Lock()
		if client.status == ONLINE {
			if time.Now().UnixNano() / 1e6 - client.lastRecvTime > this.timeout {
				log("服务器超时1\n")
				client.Conn.Close()
				client.status = OFFLIEN
				client.lckStatus.Unlock()
				break
			}
		}else{
			log("服务器错误2\n")
			client.lckStatus.Unlock()
			break
		}
		client.lckStatus.Unlock()

		if this.timeout < 5 * 1000 {
			time.Sleep(time.Duration(1000) * time.Millisecond)
		}else{
			time.Sleep(time.Duration(5 * 1000) * time.Millisecond)
		}
		
	}



}

func (this *Server)serverClientWork(conn net.Conn) {

	client := &Client{Conn:conn}
	client.lastRecvTime = time.Now().UnixNano() / 1e6
	client.status = ONLINE

	//客户端连接上来触发
	if this.onCreate != nil {
		this.onCreate(client)
	}

	//开启心跳检测 服务器的心跳检测不主动发送心跳包  靠客户端发心跳然后服务器反包这种机制
	if this.timeout != 0 {
		go this.serverClientHeart(client)
	}

	//收包
	lengthBuf := make([]byte, 4)
	for  {
		_, err := io.ReadFull(conn, lengthBuf)
		if err != nil {
			log("服务器错误1\n")
			break
		}
		length := binary.LittleEndian.Uint32(lengthBuf)

		var bodyBuf []byte
		if length > 4 {
			bodyBuf = make([]byte, length)
			copy(bodyBuf[0:4], lengthBuf)
			_, err = io.ReadFull(conn, bodyBuf[4:])
			if err != nil {
				log("服务器错误2\n")
				break
			}
		}
		//这里已经收到一个完整的包
		client.lastRecvTime = time.Now().UnixNano() / 1e6
		if length == 4 {
			//只有长度是心跳 回一个
			log("server收到心跳包\n")
			if this.timeout != 0 {
				conn.Write(lengthBuf)
			}

		}else{
			log("server收到普通包\n")
			if this.onRecv != nil {
				this.onRecv(client, &bodyBuf)
			}
		}
	}






	//退出
	client.lckStatus.Lock()
	if client.status == ONLINE {
		client.status = OFFLIEN
		client.Conn.Close()
	}
	client.lckStatus.Unlock()

	//结束时候触发
	if this.onClose != nil {
		this.onClose(client)
	}
}



func (this *Client)clientHeartSend() {

	sendTime := this.lastRecvTime
	heartPack := []byte{4, 0, 0, 0}
	for {
		this.lckStatus.Lock()
		if this.status == ONLINE {
			if time.Now().UnixNano() / 1e6 - sendTime > this.timeout {
				//发送一个心跳包
				sendTime = time.Now().UnixNano() / 1e6
				this.Conn.Write(heartPack)
			}
		}else{
			this.lckStatus.Unlock()
			break
		}
		this.lckStatus.Unlock()

		if this.timeout < 3 * 1000 {
			time.Sleep(time.Duration(1000) * time.Millisecond)
		}else{
			time.Sleep(time.Duration(3 * 1000) * time.Millisecond)
		}

	}
	log("客户端发送心跳结束\n")
}

func (this *Client)clientHeart() {


	for {
		this.lckStatus.Lock()
		if this.status == ONLINE {
			//客户端延长验证一点
			if time.Now().UnixNano() / 1e6 - this.lastRecvTime > (this.timeout + 30 * 1000) {
				log("客户端超时\n")
				this.Conn.Close()
				this.status = OFFLIEN
				this.lckStatus.Unlock()
				break
			}
		}else{
			log("客户端超时2\n")
			this.lckStatus.Unlock()
			break
		}
		this.lckStatus.Unlock()

		if this.timeout < 3 * 1000 {
			time.Sleep(time.Duration(1000) * time.Millisecond)
		}else{
			time.Sleep(time.Duration(3 * 1000) * time.Millisecond)
		}

	}

}

func (this *Client)sclientWork() {


	this.lastRecvTime = time.Now().UnixNano() / 1e6
	this.status = ONLINE

	//客户端连接上来触发
	if this.onCreate != nil {
		this.onCreate(this)
	}

	//开启心跳检测 服务器的心跳检测不主动发送心跳包  靠客户端发心跳然后服务器反包这种机制
	if this.timeout != 0 {
		go this.clientHeart() //检测心跳
		go this.clientHeartSend() ///发送心跳
	}

	//收包
	lengthBuf := make([]byte, 4)
	for  {
		_, err := io.ReadFull(this.Conn, lengthBuf)
		if err != nil {
			log("客户端错误\n")
			break
		}
		length := binary.LittleEndian.Uint32(lengthBuf)

		var bodyBuf []byte
		if length > 4 {
			bodyBuf = make([]byte, length)
			copy(bodyBuf[0:4], lengthBuf)
			_, err = io.ReadFull(this.Conn, bodyBuf[4:])
			if err != nil {
				log("客户端错误2\n")
				break
			}
		}
		//这里已经收到一个完整的包 重置心跳时间
		this.lastRecvTime = time.Now().UnixNano() / 1e6
		if length == 4 {
			//只有长度是心跳
			log("client收到心跳包\n")
		}else{
			log("client收到普通包包\n")
			if this.onRecv != nil {
				this.onRecv(this, &bodyBuf)
			}
		}
	}


	//退出
	this.lckStatus.Lock()
	if this.status == ONLINE {
		this.status = OFFLIEN
		this.Conn.Close()
	}
	this.lckStatus.Unlock()

	//结束时候触发
	if this.onClose != nil {
		this.onClose(this)
	}
}

////////////////////////////////////////////客户端///////////////////////////////////////////////////
func (this *Client)Connect(address string) error{
	tcpAddr, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return err
	}
	// 连接
	this.Conn, err = net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		return err
	}

	go this.sclientWork()

	return nil
}
func (this *Client)SetOnCreateHandle(h func(* Client)){
	this.onCreate = h
}

func (this *Client)SetOnCloseHandle(h func(* Client)){
	this.onClose = h
}

func (this *Client)SetOnRecvHandle(h func(* Client, *[]byte)){
	this.onRecv = h
}

//发送心跳包和超时的时间。比如设置 20 * 1000  就是20秒发送一个心跳包
//超时时间是 20 + 30(固定)还没有收到服务器的任何回包 结束
func (this *Client)SetDeadline(timeout int64){
	this.timeout = timeout
}

func (this *Client)Close(){
	this.Conn.Close()
}

func (this *Client)Send(buf []byte){
	var dataLen int = 0
	if buf != nil {
		dataLen = len(buf)
	}

	sendBuf := make([]byte, 4 + dataLen)

	binary.LittleEndian.PutUint32(sendBuf[0:4], 4 + uint32(dataLen))
	if dataLen != 0 {
		copy(sendBuf[4:], buf)
	}
	this.Conn.Write(sendBuf)
}

func (this *Client)SetData(data interface{}){
	this.data = data
}

func (this *Client)GetData() interface{}{
	return this.data
}

func (this *Client)GetStatus() int{
	return this.status
}



//////////////////////////////////////////////服务器//////////////////////////////////////////////
//有客户端连上来回调函数
func (this *Server)SetOnCreateHandle(h func(* Client)){
	this.onCreate = h
}

//有客户端下线 回调函数
func (this *Server)SetOnCloseHandle(h func(* Client)){
	this.onClose = h
}

//收到一个完整的包 回调函数  第一个int32必须存放整个包的长度 包括int32的4个字节
func (this *Server)SetOnRecvHandle(h func(* Client, *[]byte)){
	this.onRecv = h
}

//timeout  毫秒  多少毫秒没有遇到一个完整的包到达服务器 断开连接
//如果没调用该函数 用不超时
func (this *Server)SetDeadline(timeout int64){
	this.timeout = timeout
}

func (this *Server)Listen(address string) bool {
	listener,err := net.Listen("tcp", address)
	if err != nil {
		log("%v\n", err)
		return false
	}
	defer listener.Close()
	for  {
		conn, err := listener.Accept()
		if err != nil {
			log("%v\n", err)
			return false
		}

		go this.serverClientWork(conn)
	}
}



