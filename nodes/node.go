package nodes

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	pb "github.com/Xerzes01/Auction/grpc"

	"google.golang.org/grpc"
)

type AuctionNode struct {
	pb.UnimplementedAuctionServiceServer

	ID        int
	Port      int
	Peers     []string
	Highest   int32
	Bidder    string
	StartTime time.Time
	Duration  time.Duration
	Ended     bool
	Mutex     sync.Mutex
}

func NewAuctionNode(id int, port int, peers []string) *AuctionNode {
	return &AuctionNode{
		ID:        id,
		Port:      port,
		Peers:     peers,
		StartTime: time.Now(),
		Duration:  time.Second * 100, // Auction duration
	}
}

func ParsePeers(peers string) []string {
	if peers == "" {
		return []string{}
	}
	return strings.Split(peers, ",")
}

func (n *AuctionNode) Start() {
	addr := fmt.Sprintf(":%d", n.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterAuctionServiceServer(grpcServer, n)

	log.Printf("Auction node %d running on %s", n.ID, addr)
	if len(n.Peers) > 0 {
		log.Printf("Peers: %v", n.Peers)
	}

	// Goroutine to end auction automatically
	go func() {
		time.Sleep(n.Duration)
		n.Mutex.Lock()
		n.Ended = true
		n.Mutex.Unlock()
		log.Println("Auction ended")
	}()

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func (n *AuctionNode) PlaceBid(ctx context.Context, bid *pb.Bid) (*pb.Ack, error) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	if n.Ended || time.Since(n.StartTime) > n.Duration {
		n.Ended = true
		return &pb.Ack{Message: "Auction has ended"}, nil
	}

	if bid.Amount <= n.Highest {
		return &pb.Ack{Message: "Bid too low"}, nil
	}

	// Tentatively accept bid
	n.Highest = bid.Amount
	n.Bidder = bid.Bidder

	success := n.ReplicateBid(bid)
	if !success {
		return &pb.Ack{Message: "Bid failed replication"}, nil
	}

	return &pb.Ack{Message: "Bid accepted!"}, nil
}

func (n *AuctionNode) GetResult(ctx context.Context, _ *pb.Empty) (*pb.Result, error) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	if time.Since(n.StartTime) >= n.Duration {
		n.Ended = true
	}

	return &pb.Result{
		Amount: n.Highest,
		Bidder: n.Bidder,
	}, nil
}

// ReplicateBid sends the bid to peers and waits for majority success
func (n *AuctionNode) ReplicateBid(bid *pb.Bid) bool {
	successCount := 1 // count self

	for _, peer := range n.Peers {
		conn, client := ConnectToNode(peer)
		if conn == nil {
			continue
		}
		defer conn.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		ack, err := client.PlaceBid(ctx, bid)
		cancel()
		if err == nil && ack.Message == "Bid accepted!" {
			successCount++
		}
	}

	// Majority quorum
	if successCount >= (len(n.Peers)+1)/2+1 {
		return true
	}
	return false
}

func ConnectToNode(addr string) (*grpc.ClientConn, pb.AuctionServiceClient) {
	conn, err := grpc.Dial(addr, grpc.WithInsecure())
	if err != nil {
		log.Println("Failed to connect to", addr, err)
		return nil, nil
	}
	client := pb.NewAuctionServiceClient(conn)
	return conn, client
}
