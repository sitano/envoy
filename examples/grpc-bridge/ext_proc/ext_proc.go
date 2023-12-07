// $ bazel-bin/source/exe/envoy-static --config-path ./envoy-proxy.yaml --log-level debug --base-id 2
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	pb "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
)

var (
	grpcport = flag.String("grpcport", ":8082", "grpcport")
	delay    = flag.Duration("delay", time.Duration(0), "response delay")
	hs       *health.Server
)

const ()

type server struct{}

type healthServer struct{}

func (s *healthServer) Check(ctx context.Context, in *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	log.Printf("Handling grpc Check request + %s", in.String())
	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}, nil
}

func (s *healthServer) Watch(in *healthpb.HealthCheckRequest, srv healthpb.Health_WatchServer) error {
	return status.Error(codes.Unimplemented, "Watch is not implemented")
}

func (s *server) Process(srv pb.ExternalProcessor_ProcessServer) error {
	log.Println("Got stream:  -->  ")

	ctx := srv.Context()
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		log.Printf("metadata: %+v", md)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		req, err := srv.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return status.Errorf(codes.Unknown, "cannot receive stream request: %v", err)
		}

		time.Sleep(*delay)

		resp := &pb.ProcessingResponse{}
		switch v := req.Request.(type) {
		case *pb.ProcessingRequest_RequestHeaders:
			log.Printf("pb.ProcessingRequest_RequestHeaders %+v \n", v)

			r := req.Request
			h := r.(*pb.ProcessingRequest_RequestHeaders)

			for _, n := range h.RequestHeaders.Headers.Headers {
				value := n.Value
				if len(value) == 0 {
					value = string(n.RawValue)
				}
				log.Printf("Header %s=%s", n.Key, value)
			}

			resp = &pb.ProcessingResponse{
				Response: &pb.ProcessingResponse_RequestHeaders{
					RequestHeaders: &pb.HeadersResponse{
						Response: &pb.CommonResponse{
							HeaderMutation: &pb.HeaderMutation{
								SetHeaders: []*core.HeaderValueOption{{Header: &core.HeaderValue{Key: "x-from-ext-proc-srv", RawValue: []byte("1")}}},
							},
						},
					},
				},
			}
			break

		case *pb.ProcessingRequest_RequestBody:
			r := req.Request
			b := r.(*pb.ProcessingRequest_RequestBody)

			log.Printf("   RequestBody: %s", string(b.RequestBody.Body))
			log.Printf("   EndOfStream: %T", b.RequestBody.EndOfStream)

			if b.RequestBody.EndOfStream {
				resp = &pb.ProcessingResponse{
					Response: &pb.ProcessingResponse_RequestBody{
						RequestBody: &pb.BodyResponse{
							Response: &pb.CommonResponse{
								HeaderMutation: &pb.HeaderMutation{
									SetHeaders: []*core.HeaderValueOption{{Header: &core.HeaderValue{Key: "x-from-ext-proc-srv", RawValue: []byte("2")}}},
								},
							},
						},
					},
				}
			}
			break

		case *pb.ProcessingRequest_ResponseHeaders:
			log.Printf("pb.ProcessingRequest_ResponseHeaders %v \n", v)
			r := req.Request
			_ = r.(*pb.ProcessingRequest_ResponseHeaders)

			resp = &pb.ProcessingResponse{
				Response: &pb.ProcessingResponse_ResponseHeaders{
					ResponseHeaders: &pb.HeadersResponse{
						Response: &pb.CommonResponse{
							HeaderMutation: &pb.HeaderMutation{
								SetHeaders: []*core.HeaderValueOption{{Header: &core.HeaderValue{Key: "x-from-ext-proc-srv", RawValue: []byte("3")}}},
							},
						},
					},
				},
			}
			break

		case *pb.ProcessingRequest_ResponseBody:
			log.Printf("pb.ProcessingRequest_ResponseBody %v \n", v)
			r := req.Request
			b := r.(*pb.ProcessingRequest_ResponseBody)
			if b.ResponseBody.EndOfStream {
				resp = &pb.ProcessingResponse{
					Response: &pb.ProcessingResponse_ResponseBody{
						ResponseBody: &pb.BodyResponse{
							Response: &pb.CommonResponse{
								HeaderMutation: &pb.HeaderMutation{
									SetHeaders: []*core.HeaderValueOption{{Header: &core.HeaderValue{Key: "x-from-ext-proc-srv", RawValue: []byte("4")}}},
								},
							},
						},
					},
				}
			}

			break

		default:
			log.Printf("Unknown Request type %v\n", v)
		}
		log.Printf("Response %+v \n", resp)
		if err := srv.Send(resp); err != nil {
			log.Printf("send error %v", err)
		}
	}
}

func main() {
	flag.Parse()

	lis, err := net.Listen("tcp", *grpcport)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	sopts := []grpc.ServerOption{grpc.MaxConcurrentStreams(1000)}
	s := grpc.NewServer(sopts...)

	pb.RegisterExternalProcessorServer(s, &server{})
	healthpb.RegisterHealthServer(s, &healthServer{})

	log.Printf("Starting gRPC server on port %s\n", *grpcport)

	var gracefulStop = make(chan os.Signal, 1)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)
	go func() {
		sig := <-gracefulStop
		log.Printf("caught sig: %+v", sig)
		log.Println("Wait for 1 second to finish processing")
		time.Sleep(1 * time.Second)
		os.Exit(0)
	}()
	s.Serve(lis)
}
