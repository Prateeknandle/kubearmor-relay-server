// SPDX-License-Identifier: Apache-2.0
// Copyright 2021 Authors of KubeArmor

// Package server exports kubearmor logs
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	pb "github.com/kubearmor/KubeArmor/protobuf"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	kl "github.com/kubearmor/kubearmor-relay-server/relay-server/common"
	cfg "github.com/kubearmor/kubearmor-relay-server/relay-server/config"
	kg "github.com/kubearmor/kubearmor-relay-server/relay-server/log"
)

// ============ //
// == Global == //
// ============ //

// Running flag
var Running bool

// ClientList Map
var ClientList map[string]int

// ClientListLock Lock
var ClientListLock *sync.Mutex

func init() {
	Running = true
	ClientList = map[string]int{}
	ClientListLock = &sync.Mutex{}

}

// ========== //
// == gRPC == //
// ========== //

// MsgStruct Structure
type MsgStruct struct {
	Filter    string
	Broadcast chan *pb.Message
}

// MsgStructs Map
var MsgStructs map[string]MsgStruct

// MsgLock Lock
var MsgLock *sync.RWMutex

// AlertStruct Structure
type AlertStruct struct {
	Filter    string
	Broadcast chan *pb.Alert
}

// AlertStructs Map
var AlertStructs map[string]AlertStruct

// AlertLock Lock
var AlertLock *sync.RWMutex

// LogStruct Structure
type LogStruct struct {
	Filter    string
	Broadcast chan *pb.Log
}

// LogStructs Map
var LogStructs map[string]LogStruct

// LogLock Lock
var LogLock *sync.RWMutex

// LogService Structure
type LogService struct {
	//
}

// HealthCheck Function
func (ls *LogService) HealthCheck(ctx context.Context, nonce *pb.NonceMessage) (*pb.ReplyMessage, error) {
	replyMessage := pb.ReplyMessage{Retval: nonce.Nonce}
	return &replyMessage, nil
}

// addMsgStruct Function
func (ls *LogService) addMsgStruct(uid string, conn chan *pb.Message, filter string) {
	MsgLock.Lock()
	defer MsgLock.Unlock()

	msgStruct := MsgStruct{}
	msgStruct.Filter = filter
	msgStruct.Broadcast = conn

	MsgStructs[uid] = msgStruct

	kg.Printf("Added a new client (%s) for WatchMessages", uid)
}

// removeMsgStruct Function
func (ls *LogService) removeMsgStruct(uid string) {
	MsgLock.Lock()
	defer MsgLock.Unlock()

	delete(MsgStructs, uid)

	kg.Printf("Deleted the client (%s) for WatchMessages", uid)
}

// WatchMessages Function
func (ls *LogService) WatchMessages(req *pb.RequestMessage, svr pb.LogService_WatchMessagesServer) error {
	uid := uuid.Must(uuid.NewRandom()).String()
	conn := make(chan *pb.Message, 1000)
	defer close(conn)
	ls.addMsgStruct(uid, conn, req.Filter)
	defer ls.removeMsgStruct(uid)

	for Running {
		select {
		case <-svr.Context().Done():
			return nil
		case resp := <-conn:
			if status, ok := status.FromError(svr.Send(resp)); ok {
				switch status.Code() {
				case codes.OK:
					// noop
				case codes.Unavailable, codes.Canceled, codes.DeadlineExceeded:
					kg.Warnf("relay failed to send a message=[%+v] err=[%s]", resp, status.Err().Error())
					return status.Err()
				default:
					return nil
				}
			}
		}
	}

	return nil
}

// addAlertStruct Function
func (ls *LogService) addAlertStruct(uid string, conn chan *pb.Alert, filter string) {
	AlertLock.Lock()
	defer AlertLock.Unlock()

	alertStruct := AlertStruct{}
	alertStruct.Filter = filter
	alertStruct.Broadcast = conn

	AlertStructs[uid] = alertStruct

	kg.Printf("Added a new client (%s, %s) for WatchAlerts", uid, filter)
}

// removeAlertStruct Function
func (ls *LogService) removeAlertStruct(uid string) {
	AlertLock.Lock()
	defer AlertLock.Unlock()

	delete(AlertStructs, uid)

	kg.Printf("Deleted the client (%s) for WatchAlerts", uid)
}

// WatchAlerts Function
func (ls *LogService) WatchAlerts(req *pb.RequestMessage, svr pb.LogService_WatchAlertsServer) error {
	uid := uuid.Must(uuid.NewRandom()).String()

	if req.Filter != "all" && req.Filter != "policy" {
		return nil
	}
	conn := make(chan *pb.Alert, 1000)
	defer close(conn)
	ls.addAlertStruct(uid, conn, req.Filter)
	defer ls.removeAlertStruct(uid)

	for Running {
		select {
		case <-svr.Context().Done():
			return nil
		case resp := <-conn:
			if status, ok := status.FromError(svr.Send(resp)); ok {
				switch status.Code() {
				case codes.OK:
					// noop
				case codes.Unavailable, codes.Canceled, codes.DeadlineExceeded:
					kg.Warnf("relay failed to send an alert=[%+v] err=[%s]", resp, status.Err().Error())
					return status.Err()
				default:
					return nil
				}
			}
		}
	}

	return nil
}

// addLogStruct Function
func (ls *LogService) addLogStruct(uid string, conn chan *pb.Log, filter string) {
	LogLock.Lock()
	defer LogLock.Unlock()

	logStruct := LogStruct{}
	logStruct.Filter = filter
	logStruct.Broadcast = conn

	LogStructs[uid] = logStruct

	kg.Printf("Added a new client (%s, %s) for WatchLogs", uid, filter)
}

// removeLogStruct Function
func (ls *LogService) removeLogStruct(uid string) {
	LogLock.Lock()
	defer LogLock.Unlock()

	delete(LogStructs, uid)

	kg.Printf("Deleted the client (%s) for WatchLogs", uid)
}

// WatchLogs Function
func (ls *LogService) WatchLogs(req *pb.RequestMessage, svr pb.LogService_WatchLogsServer) error {
	uid := uuid.Must(uuid.NewRandom()).String()

	if req.Filter != "all" && req.Filter != "system" {
		return nil
	}
	conn := make(chan *pb.Log, 10000)
	defer close(conn)
	ls.addLogStruct(uid, conn, req.Filter)
	defer ls.removeLogStruct(uid)

	for Running {
		select {
		case <-svr.Context().Done():
			return nil
		case resp := <-conn:
			if status, ok := status.FromError(svr.Send(resp)); ok {
				switch status.Code() {
				case codes.OK:
					// noop
				case codes.Unavailable, codes.Canceled, codes.DeadlineExceeded:
					kg.Warnf("relay failed to send a log=[%+v] err=[%s]", resp, status.Err().Error())
					return status.Err()
				default:
					return nil
				}
			}
		}
	}

	return nil
}

// =============== //
// == Log Feeds == //
// =============== //

// LogClient Structure
type LogClient struct {
	// flag
	Running bool

	// Server
	Server string

	// connection
	conn *grpc.ClientConn

	// client
	client pb.LogServiceClient

	// messages
	MsgStream pb.LogService_WatchMessagesClient

	// alerts
	AlertStream pb.LogService_WatchAlertsClient

	// logs
	LogStream pb.LogService_WatchLogsClient

	// wait group
	WgServer *errgroup.Group

	Context context.Context
}

// NewClient Function
func NewClient(server string) *LogClient {
	var err error

	lc := &LogClient{}

	lc.Running = true

	// == //

	lc.Server = server
	var creds credentials.TransportCredentials
	if cfg.GlobalConfig.TLSEnabled {
		creds, err = loadTLSClientCredentials()
		if err != nil {
			kg.Errf("cannot load TLS credentials: ", err)
			return nil
		}
	} else {
		creds = insecure.NewCredentials()
	}

	lc.conn, err = grpc.Dial(lc.Server, grpc.WithTransportCredentials(creds))
	if err != nil {
		kg.Warnf("Failed to connect to KubeArmor's gRPC service (%s)", server)
		return nil
	}
	defer func() {
		if err != nil {
			err = lc.DestroyClient()
			if err != nil {
				kg.Warnf("DestroyClient() failed err=%s", err.Error())
			}
		}
	}()

	lc.client = pb.NewLogServiceClient(lc.conn)

	// == //

	msgIn := pb.RequestMessage{}
	msgIn.Filter = "all"

	lc.MsgStream, err = lc.client.WatchMessages(context.Background(), &msgIn)
	if err != nil {
		kg.Warnf("Failed to call WatchMessages (%s) err=%s\n", server, err.Error())
		return nil
	}

	// == //

	alertIn := pb.RequestMessage{}
	alertIn.Filter = "policy"

	lc.AlertStream, err = lc.client.WatchAlerts(context.Background(), &alertIn)
	if err != nil {
		kg.Warnf("Failed to call WatchAlerts (%s) err=%s\n", server, err.Error())
		return nil
	}

	// == //

	logIn := pb.RequestMessage{}
	logIn.Filter = "system"

	lc.LogStream, err = lc.client.WatchLogs(context.Background(), &logIn)
	if err != nil {
		kg.Warnf("Failed to call WatchLogs (%s)\n err=%s", server, err.Error())
		return nil
	}
	// == //

	// set wait group
	lc.WgServer, lc.Context = errgroup.WithContext(context.Background())

	return lc
}

// DoHealthCheck Function
func (lc *LogClient) DoHealthCheck() bool {
	// #nosec
	randNum := rand.Int31()

	// send a nonce
	nonce := pb.NonceMessage{Nonce: randNum}
	res, err := lc.client.HealthCheck(context.Background(), &nonce)
	if err != nil {
		kg.Warnf("Failed to check the liveness of KubeArmor's gRPC service (%s)", lc.Server)
		return false
	}

	// check nonce
	if randNum != res.Retval {
		return false
	}

	return true
}

// WatchMessages Function
func (lc *LogClient) WatchMessages(ctx context.Context) error {

	var err error

	for lc.Running {
		var res *pb.Message

		if res, err = lc.MsgStream.Recv(); err != nil {
			return fmt.Errorf("failed to receive a message (%s) %s", lc.Server, err.Error())

		}
		select {
		case MsgBufferChannel <- res:
		case <-ctx.Done():
			// The context is over, stop processing results
			return nil
		default:
			//not able to add it to Log buffer
		}
	}

	kg.Print("Stopped watching messages from " + lc.Server)

	return nil
}

// AddMsgFromBuffChan Adds Msg from MsgBufferChannel into MsgStructs
func (rs *RelayServer) AddMsgFromBuffChan() {

	for Running {
		select {
		case res := <-MsgBufferChannel:
			msg := pb.Message{}

			if err := kl.Clone(*res, &msg); err != nil {
				kg.Warnf("Failed to clone a message (%v)", *res)
				continue
			}
			if stdoutmsg {
				tel, _ := json.Marshal(msg)
				fmt.Printf("%s\n", string(tel))
			}
			MsgLock.RLock()
			for uid := range MsgStructs {
				select {
				case MsgStructs[uid].Broadcast <- (&msg):
				default:
				}
			}
			MsgLock.RUnlock()

		default:
			time.Sleep(time.Millisecond * 10)
		}

	}
}

// WatchAlerts Function
func (lc *LogClient) WatchAlerts(ctx context.Context) error {

	var err error

	for lc.Running {
		var res *pb.Alert

		if res, err = lc.AlertStream.Recv(); err != nil {
			return fmt.Errorf("failed to receive a alert (%s) %s", lc.Server, err.Error())
		}

		select {
		case AlertBufferChannel <- res:
		case <-ctx.Done():
			// The context is over, stop processing results
			return nil
		default:
			//not able to add it to Log buffer
		}
	}

	kg.Print("Stopped watching alerts from " + lc.Server)

	return nil
}

// AddAlertFromBuffChan Adds ALert from AlertBufferChannel into AlertStructs
func (rs *RelayServer) AddAlertFromBuffChan() {

	for Running {
		select {
		case res := <-AlertBufferChannel:
			alert := pb.Alert{}

			if err := kl.Clone(*res, &alert); err != nil {
				kg.Warnf("Failed to clone an alert (%v)", *res)
				continue
			}
			if stdoutalerts {
				tel, _ := json.Marshal(alert)
				fmt.Printf("%s\n", string(tel))
			}
			AlertLock.RLock()
			for uid := range AlertStructs {
				select {
				case AlertStructs[uid].Broadcast <- (&alert):
				default:
				}
			}
			AlertLock.RUnlock()

		default:
			time.Sleep(time.Millisecond * 10)
		}

	}
}

// WatchLogs Function
func (lc *LogClient) WatchLogs(ctx context.Context) error {

	var err error

	for lc.Running {
		var res *pb.Log

		if res, err = lc.LogStream.Recv(); err != nil {
			return fmt.Errorf("failed to receive a log (%s) %s", lc.Server, err.Error())
		}

		select {
		case LogBufferChannel <- res:
		case <-ctx.Done():
			// The context is over, stop processing results
			return nil
		default:
			//not able to add it to Log buffer
		}
	}

	kg.Print("Stopped watching logs from " + lc.Server)

	return nil
}

// AddLogFromBuffChan Adds Log from LogBufferChannel into LogStructs
func (rs *RelayServer) AddLogFromBuffChan() {

	for Running {
		select {
		case res := <-LogBufferChannel:
			log := pb.Log{}
			if err := kl.Clone(*res, &log); err != nil {
				kg.Warnf("Failed to clone a log (%v)", *res)
			}
			if stdoutlogs {
				tel, _ := json.Marshal(log)
				fmt.Printf("%s\n", string(tel))
			}
			for uid := range LogStructs {
				select {
				case LogStructs[uid].Broadcast <- (&log):
				default:
				}
			}
		default:
			time.Sleep(time.Millisecond * 10)
		}

	}
}

// DestroyClient Function
func (lc *LogClient) DestroyClient() error {
	lc.Running = false

	err := lc.conn.Close()

	return err
}

// ================== //
// == Relay Server == //
// ================== //

// RelayServer Structure
type RelayServer struct {
	// port
	Port string

	// gRPC listener
	Listener net.Listener

	// log server
	LogServer *grpc.Server

	// wait group
	WgServer sync.WaitGroup
}

// LogBufferChannel store incoming data from log stream in buffer
var LogBufferChannel chan *pb.Log

// MsgBufferChannel store incoming data from Alert stream in buffer
var MsgBufferChannel chan *pb.Message

// AlertBufferChannel store incoming data from msg stream in buffer
var AlertBufferChannel chan *pb.Alert

// NewRelayServer Function
func NewRelayServer(port string) *RelayServer {
	rs := &RelayServer{}

	rs.Port = port

	LogBufferChannel = make(chan *pb.Log, 10000)
	AlertBufferChannel = make(chan *pb.Alert, 1000)
	MsgBufferChannel = make(chan *pb.Message, 100)

	// listen to gRPC port
	listener, err := net.Listen("tcp", ":"+rs.Port)
	if err != nil {
		kg.Errf("Failed to listen a port (%s)\n", rs.Port)
		return nil
	}
	rs.Listener = listener

	kaep := keepalive.EnforcementPolicy{
		PermitWithoutStream: true,
	}

	kasp := keepalive.ServerParameters{
		Time: 5 * time.Second,
	}

	// create a log server
	var creds credentials.TransportCredentials
	if cfg.GlobalConfig.TLSEnabled {
		creds, err = loadTLSServerCredentials()
		if err != nil {
			kg.Errf("cannot load TLS credentials: ", err)
			return nil
		}
		rs.LogServer = grpc.NewServer(grpc.Creds(creds), grpc.KeepaliveEnforcementPolicy(kaep), grpc.KeepaliveParams(kasp))
	} else {
		rs.LogServer = grpc.NewServer(grpc.KeepaliveEnforcementPolicy(kaep), grpc.KeepaliveParams(kasp))
	}

	// register a log service
	logService := &LogService{}
	pb.RegisterLogServiceServer(rs.LogServer, logService)

	// initialize msg structs
	MsgStructs = make(map[string]MsgStruct)
	MsgLock = &sync.RWMutex{}

	// initialize alert structs
	AlertStructs = make(map[string]AlertStruct)
	AlertLock = &sync.RWMutex{}

	// initialize log structs
	LogStructs = make(map[string]LogStruct)
	LogLock = &sync.RWMutex{}

	// set wait group
	rs.WgServer = sync.WaitGroup{}

	return rs
}

// DestroyRelayServer Function
func (rs *RelayServer) DestroyRelayServer() error {
	// stop gRPC service
	Running = false

	// wait for a while
	time.Sleep(time.Second * 1)

	// close listener
	if rs.Listener != nil {
		if err := rs.Listener.Close(); err != nil {
			kg.Err(err.Error())
		}
		rs.Listener = nil
	}

	// wait for other routines
	rs.WgServer.Wait()

	return nil
}

// =============== //
// == Log Feeds == //
// =============== //

// ServeLogFeeds Function
func (rs *RelayServer) ServeLogFeeds() {
	rs.WgServer.Add(1)
	defer rs.WgServer.Done()

	// feed logs
	if err := rs.LogServer.Serve(rs.Listener); err != nil {
		kg.Print("Terminated the gRPC service")
	}
}

// DeleteClientEntry removes nodeIP from ClientList
func DeleteClientEntry(nodeIP string) {
	ClientListLock.Lock()
	defer ClientListLock.Unlock()

	_, exists := ClientList[nodeIP]

	if exists {
		delete(ClientList, nodeIP)
	}
}

// =============== //
// == KubeArmor == //
// =============== //

func connectToKubeArmor(nodeIP, port string) error {

	// create connection info
	server := nodeIP + ":" + port

	defer DeleteClientEntry(nodeIP)

	// create a client
	client := NewClient(server)
	if client == nil {
		return nil
	}

	// do healthcheck
	if ok := client.DoHealthCheck(); !ok {
		kg.Warnf("Failed to check the liveness of KubeArmor's gRPC service (%s)", server)
		return nil
	}
	kg.Printf("Checked the liveness of KubeArmor's gRPC service (%s)", server)

	// watch messages
	client.WgServer.Go(func() error {
		return client.WatchMessages(client.Context)
	})
	kg.Print("Started to watch messages from " + server)

	// watch alerts
	client.WgServer.Go(func() error {
		return client.WatchAlerts(client.Context)
	})
	kg.Print("Started to watch alerts from " + server)

	// watch logs
	client.WgServer.Go(func() error {
		return client.WatchLogs(client.Context)
	})
	kg.Print("Started to watch logs from " + server)

	if err := client.WgServer.Wait(); err != nil {
		kg.Warn(err.Error())
	}

	if err := client.DestroyClient(); err != nil {
		kg.Warnf("Failed to destroy the client (%s) %s", server, err.Error())
	}
	kg.Printf("Destroyed the client (%s)", server)

	return nil
}

// GetFeedsFromNodes Function
func (rs *RelayServer) GetFeedsFromNodes() {

	rs.WgServer.Add(1)
	defer rs.WgServer.Done()

	go rs.AddMsgFromBuffChan()
	go rs.AddAlertFromBuffChan()
	go rs.AddLogFromBuffChan()

	if K8s.InitK8sClient() {
		kg.Print("Initialized the Kubernetes client")

		for Running {
			newNodes := K8s.GetKubeArmorNodes()
			for _, nodeIP := range newNodes {
				ClientListLock.Lock()
				if _, ok := ClientList[nodeIP]; !ok {
					ClientList[nodeIP] = 1
					go connectToKubeArmor(nodeIP, rs.Port)
				}
				ClientListLock.Unlock()
			}

			time.Sleep(time.Second * 1)
		}

	}
}
