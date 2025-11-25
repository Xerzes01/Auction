package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	auction "github.com/Xerzes01/Auction/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// AuctionNode represents a node in a distributed auction system
type AuctionNode struct {
	auction.UnimplementedAuctionServiceServer // Embed for forward compatibility

	mu            sync.RWMutex
	nodeID        string
	port          int
	peers         []string
	startTime     time.Time
	duration      time.Duration

	highestBid      int32
	highestBidder   string
	lastReplicated  int64 // UnixNano timestamp of last applied state

	grpcServer      *grpc.Server
	listener        net.Listener
	clients         map[string]auction.AuctionServiceClient
	conns           map[string]*grpc.ClientConn
	shutdown        chan struct{}
}

// NewAuctionNode creates a new auction node
func NewAuctionNode(id string, port int, peers []string, duration time.Duration) *AuctionNode {
	return &AuctionNode{
		nodeID:       id,
		port:         port,
		peers:        peers,
		startTime:    time.Now(),
		duration:     duration,
		clients:      make(map[string]auction.AuctionServiceClient),
		conns:        make(map[string]*grpc.ClientConn),
		shutdown:     make(chan struct{}),
	}
}

// isAuctionOver checks if the auction duration has elapsed
func (n *AuctionNode) isAuctionOver() bool {
	return time.Since(n.startTime) >= n.duration
}

// ================================
// gRPC Service Handlers
// ================================

// Bid handles incoming bids from clients
func (n *AuctionNode) Bid(ctx context.Context, req *auction.BidRequest) (*auction.BidResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.isAuctionOver() {
		return &auction.BidResponse{
			Status:     auction.BidResponse_FAIL,
			HighestBid: n.highestBid,
		}, nil
	}

	if req.Amount <= n.highestBid {
		return &auction.BidResponse{
			Status:     auction.BidResponse_FAIL,
			HighestBid: n.highestBid,
		}, nil
	}

	// Accept new highest bid
	n.highestBid = req.Amount
	n.highestBidder = req.BidderId
	n.lastReplicated = time.Now().UnixNano()

	// Asynchronously replicate to all peers
	go n.replicateToPeers()

	log.Printf("[Node %s] New highest bid: %d by %s", n.nodeID, req.Amount, req.BidderId)

	return &auction.BidResponse{
		Status:     auction.BidResponse_SUCCESS,
		HighestBid: n.highestBid,
	}, nil
}

// Result returns current auction status
func (n *AuctionNode) Result(ctx context.Context, req *auction.ResultRequest) (*auction.ResultResponse, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	over := n.isAuctionOver()

	resp := &auction.ResultResponse{
		AuctionOver: over,
		HighestBid:  n.highestBid,
		Winner:      "None",
	}

	if over && n.highestBidder != "" {
		resp.Winner = n.highestBidder
	}

	return resp, nil
}

// ReplicateState receives state updates from other nodes
func (n *AuctionNode) ReplicateState(ctx context.Context, msg *auction.ReplicationMsg) (*auction.Ack, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Ignore stale or duplicate updates
	if msg.Timestamp <= n.lastReplicated {
		return &auction.Ack{Ok: true}, nil
	}

	// Apply only if this update is newer and has a higher (or equal) bid
	if msg.HighestBid > n.highestBid ||
		(msg.HighestBid == n.highestBid && msg.Timestamp > n.lastReplicated) {

		n.highestBid = msg.HighestBid
		n.highestBidder = msg.HighestBidder
		n.lastReplicated = msg.Timestamp

		log.Printf("[Node %s] Replicated state: %d by %s (ts=%d)",
			n.nodeID, msg.HighestBid, msg.HighestBidder, msg.Timestamp)
	}

	return &auction.Ack{Ok: true}, nil
}

// ================================
// Server Lifecycle
// ================================

// Start launches the gRPC server
func (n *AuctionNode) Start() error {
	addr := fmt.Sprintf(":%d", n.port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %v", addr, err)
	}

	n.listener = lis
	n.grpcServer = grpc.NewServer()

	auction.RegisterAuctionServiceServer(n.grpcServer, n)

	log.Printf("[Node %s] gRPC server starting on %s", n.nodeID, addr)

	go n.grpcServer.Serve(lis)
	return nil
}

// Stop gracefully shuts down the server and connections
func (n *AuctionNode) Stop() {
	if n.grpcServer != nil {
		n.grpcServer.GracefulStop()
	}

	for _, conn := range n.conns {
		conn.Close()
	}

	close(n.shutdown)
}

// ================================
// Peer Communication
// ================================

// getClient lazily creates and caches gRPC client connections to peers
func (n *AuctionNode) getClient(addr string) (auction.AuctionServiceClient, error) {
	n.mu.Lock()
	if client, ok := n.clients[addr]; ok {
		n.mu.Unlock()
		return client, nil
	}
	n.mu.Unlock()

	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	client := auction.NewAuctionServiceClient(conn)

	n.mu.Lock()
	n.clients[addr] = client
	n.conns[addr] = conn
	n.mu.Unlock()

	return client, nil
}

// replicateToPeers sends the current state to all configured peers (fire-and-forget)
func (n *AuctionNode) replicateToPeers() {
	n.mu.RLock()
	msg := &auction.ReplicationMsg{
		HighestBid:     n.highestBid,
		HighestBidder:  n.highestBidder,
		Timestamp:      n.lastReplicated,
	}
	n.mu.RUnlock()

	for _, peer := range n.peers {
		peer := strings.TrimSpace(peer)
		if peer == "" {
			continue
		}

		client, err := n.getClient(peer)
		if err != nil {
			log.Printf("[Node %s] Failed to connect to peer %s: %v", n.nodeID, peer, err)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err = client.ReplicateState(ctx, msg)
		cancel()

		if err != nil {
			log.Printf("[Node %s] Replication to %s failed: %v", n.nodeID, peer, err)
		}
	}
}

// ================================
// Utility Functions
// ================================

// ParsePeers converts a comma-separated string of addresses into a clean slice
func ParsePeers(peersStr string) []string {
	if peersStr == "" {
		return nil
	}

	var peers []string
	for _, p := range strings.Split(peersStr, ",") {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			peers = append(peers, trimmed)
		}
	}
	return peers
}