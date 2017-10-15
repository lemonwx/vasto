package gateway

import (
	"fmt"
	"log"
	"net"

	"github.com/chrislusf/vasto/cmd/client"
)

type GatewayOption struct {
	Host       *string
	TcpPort    *int32
	GrpcPort   *int32
	Master     *string
	DataCenter *string
}

type gatewayServer struct {
	option *GatewayOption

	clientToMaster *client.VastoClient
}

func RunGateway(option *GatewayOption) {

	var gs = &gatewayServer{
		option: option,
		clientToMaster: client.New(
			&client.ClientOption{
				Master:     option.Master,
				DataCenter: option.DataCenter,
			},
		),
	}

	if *option.GrpcPort != 0 {
		grpcListener, err := net.Listen("tcp", fmt.Sprintf("%v:%d", *option.Host, *option.GrpcPort))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Vasto gateway starts grpc %v:%d\n", *option.Host, *option.GrpcPort)
		go gs.serveGrpc(grpcListener)
	}

	go gs.clientToMaster.Start()

	select {}

}