package master

import (
	"io"

	"github.com/chrislusf/vasto/pb"
	"github.com/chrislusf/vasto/topology"
	"log"
)

func (ms *masterServer) RegisterStore(stream pb.VastoMaster_RegisterStoreServer) error {
	var storeHeartbeat *pb.StoreHeartbeat
	var err error

	storeHeartbeat, err = stream.Recv()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}

	log.Printf("cluster %s store %d connected %s %v %v\n", storeHeartbeat.DataCenter,
		storeHeartbeat.Store.Id,
		storeHeartbeat.Store.Network, storeHeartbeat.Store.Address, storeHeartbeat.Store.AdminAddress)

	node := topology.NewNodeFromStore(storeHeartbeat.Store)

	ms.Lock()
	ring, ok := ms.clusters[storeHeartbeat.DataCenter]
	if !ok {
		t := topology.NewHashRing(storeHeartbeat.DataCenter)
		ring = &t
		ring.SetExpectedSize(ms.defaultClusterSize)
		ms.clusters[storeHeartbeat.DataCenter] = ring
	}
	ms.Unlock()

	ring.Add(node)
	ms.clientChans.notifyStoreResourceUpdate(
		storeHeartbeat.DataCenter,
		[]*pb.StoreResource{
			storeHeartbeat.Store,
		},
		false,
	)

	storeDisconnectedChan := make(chan bool, 1)

	go func() {
		var e error
		for {
			beat, e := stream.Recv()
			if e != nil {
				break
			}
			for k, v := range storeHeartbeat.Store.NodeStatuses {
				newNodeStatus := beat.Store.NodeStatuses[k]
				if newNodeStatus.Status != v.Status {
					log.Printf("node %d status %v => %v", k, v.Status, newNodeStatus.Status)
					storeHeartbeat.Store.NodeStatuses[k] = newNodeStatus
				}
			}
			ms.clientChans.notifyStoreResourceUpdate(
				storeHeartbeat.DataCenter,
				[]*pb.StoreResource{
					storeHeartbeat.Store,
				},
				false,
			)
		}
		log.Printf("store %d disconnected %v: %v", storeHeartbeat.Store.Id, storeHeartbeat.Store.Address, e)
		storeDisconnectedChan <- true
	}()

	for {
		select {
		case <-storeDisconnectedChan:
			ring.Remove(node.GetId())
			ms.clientChans.notifyStoreResourceUpdate(
				storeHeartbeat.DataCenter,
				[]*pb.StoreResource{{
					Id:      int32(node.GetId()),
					Network: node.GetNetwork(),
					Address: node.GetAddress(),
				}},
				true,
			)
			return nil
		}
	}

	return nil
}
