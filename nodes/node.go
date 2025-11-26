package nodes

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "github.com/Xerzes01/Auction/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	AuctionDuration = 100 * time.Second
	QuorumSize      = 2
)

type Phase int

const (
	Ongoing Phase = iota
	Closed
)

type Node struct {
	pb.UnimplementedAuctionServiceServer

	id       int
	port     string
	selfAddr string
	peers    []string

	mu           sync.RWMutex
	phase        Phase
	startTime    time.Time
	highestBid   int32
	highestBidBy string
}

func NewNode(id int, port string, peers []string) *Node {
	selfAddr := "localhost:" + port

	n := &Node{
		id:         id,
		port:       port,
		selfAddr:   selfAddr,
		peers:      peers,
		phase:      Ongoing,
		startTime:  time.Now(),
		highestBid: 0,
	}

	go n.auctionTimer()

	return n
}

func (n *Node) auctionTimer() {
	<-time.After(AuctionDuration)
	n.mu.Lock()
	if n.phase == Ongoing {
		n.phase = Closed
		log.Printf("[Node %d] === AUCTION CLOSED === Winner: %s ($%d)", n.id, n.highestBidBy, n.highestBid)
	}
	n.mu.Unlock()
}

func (n *Node) StartGRPCServer() {
	lis, err := net.Listen("tcp", ":"+n.port)
	if err != nil {
		log.Fatalf("Node %d failed to listen on port %s: %v", n.id, n.port, err)
	}

	s := grpc.NewServer()
	pb.RegisterAuctionServiceServer(s, n)

	log.Printf("Node %d running on %s | Peers: %v", n.id, n.selfAddr, n.peers)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("Node %d failed: %v", n.id, err)
	}
}

// -------------------- RPCs --------------------

func (n *Node) PlaceBid(ctx context.Context, bid *pb.Bid) (*pb.Ack, error) {
	n.mu.RLock()
	if n.phase == Closed {
		n.mu.RUnlock()
		return &pb.Ack{Message: "fail: auction closed"}, nil
	}
	n.mu.RUnlock()

	// Try to commit bid locally and propagate to peers
	if !n.commitBidWithQuorum(bid) {
		return nil, status.Error(codes.Unavailable, "quorum not reached")
	}

	return &pb.Ack{Message: "success"}, nil
}

// This is called by peers for propagation, no quorum check
func (n *Node) PropagateBid(ctx context.Context, bid *pb.Bid) (*pb.Ack, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if bid.Amount > n.highestBid {
		n.highestBid = bid.Amount
		n.highestBidBy = bid.Bidder
		log.Printf("[Node %d] Propagated bid accepted: %d by %s", n.id, bid.Amount, bid.Bidder)
	}

	return &pb.Ack{Message: "success"}, nil
}

func (n *Node) GetResult(ctx context.Context, _ *pb.Empty) (*pb.Result, error) {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.highestBid == 0 {
		return &pb.Result{Bidder: "nobody", Amount: 0}, nil
	}
	return &pb.Result{
		Bidder: n.highestBidBy,
		Amount: n.highestBid,
	}, nil
}

// -------------------- Bid Commit --------------------

func (n *Node) commitBidWithQuorum(bid *pb.Bid) bool {
	var wg sync.WaitGroup
	success := make(chan bool, len(n.peers)+1)

	// Commit locally
	n.mu.Lock()
	if bid.Amount > n.highestBid {
		n.highestBid = bid.Amount
		n.highestBidBy = bid.Bidder
		log.Printf("[Node %d] Accepted bid: %d by %s", n.id, bid.Amount, bid.Bidder)
	}
	n.mu.Unlock()
	success <- true

	// Propagate to peers
	for _, peer := range n.peers {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			conn, err := grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithBlock())
			if err != nil {
				return
			}
			defer conn.Close()

			client := pb.NewAuctionServiceClient(conn)
			resp, err := client.PropagateBid(ctx, bid)
			if err == nil && resp != nil && resp.Message == "success" {
				success <- true
			}
		}(peer)
	}

	successCount := 0
	timeout := time.After(3 * time.Second)

	for successCount < QuorumSize {
		select {
		case <-success:
			successCount++
		case <-timeout:
			log.Printf("[Node %d] Quorum timeout for bid %d", n.id, bid.Amount)
			wg.Wait()
			return false
		}
	}

	wg.Wait()
	return true
}

// -------------------- Console --------------------

func (n *Node) StartConsole() {
	scanner := bufio.NewScanner(os.Stdin)
	bidderName := fmt.Sprintf("Node%d", n.id)

	for {
		fmt.Printf("[Node %d] > ", n.id)
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		parts := strings.SplitN(input, " ", 2)
		cmd := parts[0]

		switch cmd {
		case "bid":
			if len(parts) < 2 {
				fmt.Println("Usage: bid <amount>")
				continue
			}
			amount, err := strconv.Atoi(parts[1])
			if err != nil {
				fmt.Println("Invalid amount")
				continue
			}

			conn, err := grpc.Dial(n.selfAddr, grpc.WithInsecure())
			if err != nil {
				fmt.Println("Cannot connect to self")
				continue
			}
			client := pb.NewAuctionServiceClient(conn)
			resp, err := client.PlaceBid(context.Background(), &pb.Bid{
				Bidder: bidderName,
				Amount: int32(amount),
			})
			conn.Close()

			if err != nil {
				fmt.Printf("exception: %v\n", err)
			} else {
				fmt.Println("→", resp.Message)
			}

		case "result", "query":
			conn, err := grpc.Dial(n.selfAddr, grpc.WithInsecure())
			if err != nil {
				fmt.Println("Cannot query")
				continue
			}
			client := pb.NewAuctionServiceClient(conn)
			res, err := client.GetResult(context.Background(), &pb.Empty{})
			conn.Close()

			if err != nil {
				fmt.Println("exception:", err)
			} else if res.Bidder == "nobody" {
				fmt.Println("No bids yet")
			} else {
				closed := " (auction ongoing)"
				n.mu.RLock()
				if n.phase == Closed {
					closed = " ← AUCTION CLOSED!"
				}
				n.mu.RUnlock()
				fmt.Printf("Highest bid: %d by %s%s\n", res.Amount, res.Bidder, closed)
			}

		case "help":
			fmt.Println("Commands: bid <amount> | result | help | exit")

		case "exit":
			fmt.Println("Bye!")
			os.Exit(0)

		default:
			fmt.Println("Unknown command")
		}
	}
}
