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

// AuctionNode implements the AuctionService
type AuctionNode struct {
	pb.UnimplementedAuctionServiceServer

	ID        int
	Port      int
	Peers     []string
	Mutex     sync.Mutex
	Highest   int32
	Bidder    string
	StartTime time.Time
	Duration  time.Duration // auction duration from start

	// optional: dial timeout for RPCs to peers
	RPCTimeout time.Duration
}

func NewAuctionNode(id int, port int, peers []string) *AuctionNode {
	return &AuctionNode{
		ID:         id,
		Port:       port,
		Peers:      peers,
		StartTime:  time.Now(),
		Duration:   time.Second * 100, // default 100s auction length (changeable)
		RPCTimeout: time.Second * 1,
	}
}

func ParsePeers(peers string) []string {
	if peers == "" {
		return []string{}
	}
	parts := strings.Split(peers, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
	log.Printf("Auction started at %s for duration %s", n.StartTime.Format(time.RFC3339), n.Duration)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// PlaceBid handles both client bids (replicate==false) and replicated requests (replicate==true).
// For client-originated bids we attempt to replicate to peers and only commit locally once a majority acknowledges.
func (n *AuctionNode) PlaceBid(ctx context.Context, bid *pb.Bid) (*pb.Ack, error) {
	// Check if auction ended
	if n.isAuctionEnded() {
		return &pb.Ack{Message: "Auction has ended"}, nil
	}

	// Validate local condition: new bid must be greater than local highest.
	n.Mutex.Lock()
	localHighest := n.Highest
	n.Mutex.Unlock()

	if bid.Amount <= localHighest {
		return &pb.Ack{Message: "Bid too low"}, nil
	}

	// If this is a replication request (from another node), just update local state (no further replication)
	if bid.Replicate {
		n.Mutex.Lock()
		// Only accept if still higher than local value (concurrent updates may have advanced)
		if bid.Amount > n.Highest {
			n.Highest = bid.Amount
			n.Bidder = bid.Bidder
		}
		n.Mutex.Unlock()
		return &pb.Ack{Message: "Replicated"}, nil
	}

	// Otherwise: client-originated bid -> replicate to peers and commit if quorum reached.
	totalNodes := len(n.Peers) + 1
	quorum := totalNodes/2 + 1

	var wg sync.WaitGroup
	successCh := make(chan struct{}, len(n.Peers))
	// replicate to each peer concurrently
	for _, peerAddr := range n.Peers {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			conn, client, err := ConnectToNode(addr, n.RPCTimeout)
			if err != nil {
				return
			}
			// ensure connection closed immediately after use
			defer conn.Close()

			ctx, cancel := context.WithTimeout(context.Background(), n.RPCTimeout)
			defer cancel()

			// send replication request (replicate = true)
			rep := &pb.Bid{
				Bidder:    bid.Bidder,
				Amount:    bid.Amount,
				Replicate: true,
			}
			_, err = client.PlaceBid(ctx, rep)
			if err == nil {
				// peer accepted replication
				select {
				case successCh <- struct{}{}:
				default:
				}
			}
		}(peerAddr)
	}

	// Wait for all replication RPC goroutines to finish (but we can also short-circuit)
	// We'll collect successes while waiting, but to keep simple, wait and then count.
	wg.Wait()
	close(successCh)

	successes := 0
	for range successCh {
		successes++
	}
	// include this node's acceptance as 1
	successesIncludingSelf := successes + 1

	if successesIncludingSelf >= quorum {
		// commit locally
		n.Mutex.Lock()
		// double-check (concurrent accepted higher bid?) - if concurrent higher exists, we must compare
		if bid.Amount > n.Highest {
			n.Highest = bid.Amount
			n.Bidder = bid.Bidder
		}
		n.Mutex.Unlock()
		return &pb.Ack{Message: "Bid accepted!"}, nil
	}

	// Not enough nodes accepted -> fail
	return &pb.Ack{Message: "Failed to reach quorum; bid not accepted"}, nil
}

func (n *AuctionNode) GetResult(ctx context.Context, _ *pb.Empty) (*pb.Result, error) {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	return &pb.Result{
		Amount: n.Highest,
		Bidder: n.Bidder,
	}, nil
}

func (n *AuctionNode) isAuctionEnded() bool {
	return time.Since(n.StartTime) >= n.Duration
}

// ConnectToNode dials a peer and returns the connection and client. Caller must close conn.
// Uses a timeout to avoid indefinite hangs.
func ConnectToNode(addr string, timeout time.Duration) (*grpc.ClientConn, pb.AuctionServiceClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		return nil, nil, err
	}
	client := pb.NewAuctionServiceClient(conn)
	return conn, client, nil
}
