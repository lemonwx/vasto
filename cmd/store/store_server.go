package store

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/chrislusf/vasto/pb"
	"github.com/chrislusf/vasto/topology/cluster_listener"
	"github.com/chrislusf/vasto/util/on_interrupt"
	"github.com/chrislusf/vasto/util"
	"github.com/tidwall/evio"
	"encoding/binary"
	"context"
	"sync"
)

type StoreOption struct {
	Dir               *string
	Host              *string
	ListenHost        *string
	TcpPort           *int32
	Bootstrap         *bool
	DisableUnixSocket *bool
	Master            *string
	FixedCluster      *string
	DataCenter        *string
	Keyspace          *string
	LogFileSizeMb     *int
	LogFileCount      *int
	DiskSizeGb        *int
	Tags              *string
	DisableUseEventIo *bool
}

func (o *StoreOption) GetAdminPort() int32 {
	return *o.TcpPort + 10000
}

type storeServer struct {
	option              *StoreOption
	clusterListener     *cluster_listener.ClusterListener
	ShardInfoChan       chan *pb.ShardInfo
	statusInCluster     map[string]*pb.LocalShardsInCluster // saved to disk
	statusInClusterLock sync.RWMutex
	periodTasks         []PeriodicTask
	keyspaceShards      *keyspaceShards
}

func RunStore(option *StoreOption) {

	tcpAddress := fmt.Sprintf("%s:%d", *option.ListenHost, *option.TcpPort)

	ctx := context.Background()
	clusterListener := cluster_listener.NewClusterClient(*option.DataCenter, tcpAddress)

	var ss = &storeServer{
		option:          option,
		clusterListener: clusterListener,
		ShardInfoChan:   make(chan *pb.ShardInfo),
		statusInCluster: make(map[string]*pb.LocalShardsInCluster),
		keyspaceShards:  newKeyspaceShards(),
	}
	go ss.startPeriodTasks()

	// ss.clusterListener.RegisterShardEventProcessor(&cluster_listener.ClusterEventLogger{})

	if err := ss.listExistingClusters(); err != nil {
		log.Fatalf("load existing cluster files: %v", err)
	}

	if *option.FixedCluster != "" {
		clusterListener.SetNodes(*option.Keyspace, *ss.option.FixedCluster)
	} else if *option.Master != "" {
		go ss.keepConnectedToMasterServer(ctx)
		for keyspaceName, ShardInfo := range ss.statusInCluster {
			clusterListener.AddExistingKeyspace(keyspaceName, int(ShardInfo.ClusterSize), int(ShardInfo.ReplicationFactor))
		}
		clusterListener.StartListener(ctx, *ss.option.Master, *ss.option.DataCenter, false)
	}

	for keyspaceName, storeStatus := range ss.statusInCluster {
		if err := ss.startExistingNodes(keyspaceName, storeStatus); err != nil {
			log.Fatalf("load existing keyspace: %v", keyspaceName, err)
		}
	}

	if *option.TcpPort != 0 {
		grpcAddress := fmt.Sprintf("%s:%d", *option.ListenHost, option.GetAdminPort())
		grpcListener, err := net.Listen("tcp", grpcAddress)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("store admin %s", grpcAddress)
		go ss.serveGrpc(grpcListener)
	}

	if !*option.DisableUseEventIo {
		tcpAddress := fmt.Sprintf("%s:%d", *option.ListenHost, *option.TcpPort)
		unixSocket, _ := util.GetUnixSocketFile(tcpAddress)
		var events evio.Events
		var conns = make(map[int]*conn)
		events.Opened = func(id int, info evio.Info) (out []byte, opts evio.Options, action evio.Action) {
			conns[id] = &conn{info: info}
			return
		}
		events.Closed = func(id int, err error) (action evio.Action) {
			delete(conns, id)
			return
		}
		events.Data = func(id int, in []byte) (out []byte, action evio.Action) {
			if in == nil {
				return
			}
			c := conns[id]
			data := c.is.Begin(in)
			if len(data) < 4 {
				c.is.End(data)
				return
			}
			length := binary.LittleEndian.Uint32(data[0:4])
			if len(data) < 4+int(length) {
				c.is.End(data)
				return
			}
			request := data[4:4+int(length)]

			response, err := ss.handleInputOutput(request)
			if err != nil {
				log.Printf("handleInputOutput: %v", err)
				c.is.End(data[4+int(length):])
				return
			}

			buf := make([]byte, 4)

			binary.LittleEndian.PutUint32(buf, uint32(len(response)))
			out = append(buf, response...)

			c.is.End(data[4+int(length):])
			return
		}
		log.Printf("Vasto store starts on %s", *option.Dir)
		if err := evio.Serve(events, fmt.Sprintf("tcp://%s", tcpAddress), fmt.Sprintf("unix://%s", unixSocket)); err != nil {
			log.Printf("evio.Serve: %v", err)
		}
	} else {
		if *option.TcpPort != 0 {
			tcpAddress := fmt.Sprintf("%s:%d", *option.ListenHost, *option.TcpPort)
			tcpListener, err := net.Listen("tcp", tcpAddress)
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("listens on tcp %v", tcpAddress)
			go ss.serveTcp(tcpListener)
		}

		if !*option.DisableUnixSocket {
			tcpAddress := fmt.Sprintf("%s:%d", *option.ListenHost, *option.TcpPort)
			if unixSocket, _ := util.GetUnixSocketFile(tcpAddress); unixSocket != "" {
				if util.FileExists(unixSocket) {
					os.Remove(unixSocket)
				}
				unixSocketListener, err := net.Listen("unix", unixSocket)
				if err != nil {
					log.Fatal(err)
				}
				log.Printf("listens on socket %s", unixSocket)
				on_interrupt.OnInterrupt(func() {
					os.Remove(unixSocket)
				}, nil)
				defer os.Remove(unixSocket)
				go ss.serveTcp(unixSocketListener)
			}
		}
	}

	log.Printf("Vasto store starts on %s", *option.Dir)

	select {}

}

type conn struct {
	info evio.Info
	is   evio.InputStream
}
