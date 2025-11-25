package server

import (
	"context"
	"fmt"
	pb "auction/grpc"
)

type AuctionServer struct {
	pb.UnimplementedAuctionServiceServer
	highestBidder string
	highestAmount int32
}

func NewServer() *AuctionServer {
	return &AuctionServer{}
}

func (s *AuctionServer) PlaceBid(ctx context.Context, bid *pb.Bid) (*pb.Ack, error) {
	if bid.Amount > s.highestAmount {
		s.highestAmount = bid.Amount
		s.highestBidder = bid.Bidder
		return &pb.Ack{Message: fmt.Sprintf("Bid accepted: %s with %d", bid.Bidder, bid.Amount)}, nil
	}
	return &pb.Ack{Message: "Bid rejected: too low"}, nil
}

func (s *AuctionServer) GetResult(ctx context.Context, _ *pb.Empty) (*pb.Result, error) {
	return &pb.Result{Bidder: s.highestBidder, Amount: s.highestAmount}, nil
}
