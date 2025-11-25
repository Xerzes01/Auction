package nodes

import (
    "context"
    "fmt"
    "log"
    "net"
    "strings"
    "sync"
    "time"

    pb "auction/grpc"
    "google.golang.org/grpc"
)

type AuctionNode struct {
    pb.UnimplementedAuctionServiceServer

    ID       int
    Port     int
    Peers    []string
    Highest  int32
    Bidder   string
    Mutex    sync.Mutex
}

func NewAuctionNode(id int, port int, peers []string) *AuctionNode {
    return &AuctionNode{
        ID:    id,
        Port:  port,
        Peers: peers,
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

    if err := grpcServer.Serve(lis); err != nil {
        log.Fatalf("Server error: %v", err)
    }
}

func (n *AuctionNode) PlaceBid(ctx context.Context, bid *pb.Bid) (*pb.Ack, error) {
    n.Mutex.Lock()
    defer n.Mutex.Unlock()

    if bid.Amount > n.Highest {
        n.Highest = bid.Amount
        n.Bidder = bid.Bidder
        go n.ReplicateBid(bid)
        return &pb.Ack{Message: "Bid accepted!"}, nil
    }

    return &pb.Ack{Message: "Bid too low"}, nil
}

func (n *AuctionNode) GetResult(ctx context.Context, _ *pb.Empty) (*pb.Result, error) {
    n.Mutex.Lock()
    defer n.Mutex.Unlock()

    return &pb.Result{
        Amount: n.Highest,
        Bidder: n.Bidder,
    }, nil
}

func (n *AuctionNode) ReplicateBid(bid *pb.Bid) {
    for _, peer := range n.Peers {
        conn, client := ConnectToNode(peer)
        defer conn.Close()

        ctx, cancel := context.WithTimeout(context.Background(), time.Second*1)
        _, _ = client.PlaceBid(ctx, bid)
        cancel()
    }
}

func ConnectToNode(addr string) (*grpc.ClientConn, pb.AuctionServiceClient) {
    conn, _ := grpc.Dial(addr, grpc.WithInsecure())
    client := pb.NewAuctionServiceClient(conn)
    return conn, client
}
