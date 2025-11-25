package main

import (
	"net"
	"net/rpc"
	"sync"
	"time"
)

type BidRequest struct {
	BidderID string
	Amount   int
}

type BidReply struct {
	Outcome string
	Reason  string
}

type AppendArgs struct {
	BidderID string
	Amount   int
}

type AppendReply struct {
	Ack   bool
	Error string
}

type StateArgs struct{}
type StateReply struct {
	HighestAmount int
	HighestBidder string
}

type QueryReply struct {
	AuctionEnded  bool
	HighestAmount int
	HighestBidder string
	Message       string
}

type Node struct {
	mu            sync.Mutex
	ID            string
	Port          int
	Peers         []string
	StartTime     time.Time
	AuctionDur    time.Duration
	HighestAmount int
	HighestBidder string
	rpcClients    map[string]*rpc.Client
	server        *rpc.Server
	listener      net.Listener
	shutdown      chan struct{}
}

func NewNode(id string, port int, peers []string, dur time.Duration) *node {
	return &Node{
		ID:         id,
		Port:       port,
		Peers:      peers,
		StartTime:  time.Now(),
		AuctionDur: dur,
		rpcClients: make(map[string]*rpc.Client),
		shutdown:   make(chan struct{}),
	}
}

func (n *Node) auctionEnded() bool {
	return time.Since(n.StartTime) >= n.AuctionDur
}

func (n *Node) StartServer() error {
	n.server = rpc.NewServer()
	err := n.server.RegisterName("Node", n)
	if err != nil {
		return err
	}

	addr := fmt.Sprintf(":%d", n.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	n.listener = ln
	log.Printf("[%s] listening on %s", n.ID, addr)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-n.shutdown:
					return 
				default:
					continue
				}
			}
			go n.server.ServeConn(conn)
		}
	}
}