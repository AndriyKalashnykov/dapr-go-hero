package dapr

import (
	"context"
	"fmt"

	cpb "github.com/dapr/dapr/pkg/proto/common/v1"
	pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/go-logr/logr"
	"google.golang.org/protobuf/types/known/emptypb"
)

type (
	TopicEventHandler    func(ctx context.Context, in *pb.TopicEventRequest) (*pb.TopicEventResponse, error)
	RegisterEventHandler func(path string, handler TopicEventHandler)

	Server struct {
		log           logr.Logger
		handlers      map[string]TopicEventHandler
		subscriptions []*Subscription
	}

	HandlerRegistrar interface {
		RegisterTopicEventHandlers(register RegisterEventHandler)
	}
)

func NewServer(log logr.Logger) *Server {
	return &Server{
		log:           log,
		handlers:      make(map[string]TopicEventHandler),
		subscriptions: make([]*Subscription, 0, 10),
	}
}

func (s *Server) OnInvoke(ctx context.Context, in *cpb.InvokeRequest) (*cpb.InvokeResponse, error) {
	return nil, nil
}

func (s *Server) ListInputBindings(ctx context.Context, in *emptypb.Empty) (*pb.ListInputBindingsResponse, error) {
	return &pb.ListInputBindingsResponse{
		Bindings: []string{},
	}, nil
}

func (s *Server) OnBindingEvent(ctx context.Context, in *pb.BindingEventRequest) (*pb.BindingEventResponse, error) {
	return &pb.BindingEventResponse{}, nil
}

func (s *Server) Subscribe(subscriptions []*Subscription) {
	s.subscriptions = append(s.subscriptions, subscriptions...)
}

func (s *Server) ListTopicSubscriptions(ctx context.Context, in *emptypb.Empty) (*pb.ListTopicSubscriptionsResponse, error) {
	subs := make([]*pb.TopicSubscription, len(s.subscriptions))
	for i, s := range s.subscriptions {
		subs[i] = &pb.TopicSubscription{
			PubsubName: s.PubsubName,
			Topic:      s.Topic,
			Routes:     convertRoutes(s.Routes),
			Metadata:   s.Metadata,
		}
	}
	s.log.Info("ListTopicSubscriptions called", "subscriptions", subs)

	return &pb.ListTopicSubscriptionsResponse{
		Subscriptions: subs,
	}, nil
}

func convertRoutes(routes Routes) *pb.TopicRoutes {
	rules := make([]*pb.TopicRule, len(routes.Rules))
	for i, rule := range routes.Rules {
		rules[i] = &pb.TopicRule{
			Match: rule.Match,
			Path:  rule.Path,
		}
	}
	return &pb.TopicRoutes{
		Rules:   rules,
		Default: routes.Default,
	}
}

func (s *Server) RegisterTopicEventHandler(path string, handler TopicEventHandler) {
	s.handlers[path] = handler
}

func (s *Server) RegisterTopicEventHandlers(registrars ...HandlerRegistrar) {
	for _, r := range registrars {
		r.RegisterTopicEventHandlers(s.RegisterTopicEventHandler)
	}
}

func (s *Server) OnTopicEvent(ctx context.Context, in *pb.TopicEventRequest) (*pb.TopicEventResponse, error) {
	handler, ok := s.handlers[in.Path]
	if !ok {
		s.log.Error(nil, "handler not found", "path", in.Path)
		return nil, fmt.Errorf("handler not found for path %q", in.Path)
	}

	return handler(ctx, in)
}

func (s *Server) OnBulkTopicEvent(ctx context.Context, in *pb.TopicEventBulkRequest) (*pb.TopicEventBulkResponse, error) {
	entries := make([]*pb.TopicEventBulkResponseEntry, 0, len(in.Entries))
	for _, entry := range in.Entries {
		ce := entry.GetCloudEvent()
		if ce == nil {
			entries = append(entries, &pb.TopicEventBulkResponseEntry{
				EntryId: entry.EntryId,
				Status:  pb.TopicEventResponse_DROP,
			})
			continue
		}

		req := &pb.TopicEventRequest{
			Id:              ce.Id,
			Source:          ce.Source,
			Type:            ce.Type,
			SpecVersion:     ce.SpecVersion,
			DataContentType: ce.DataContentType,
			Data:            ce.Data,
			Topic:           in.Topic,
			PubsubName:      in.PubsubName,
			Path:            in.Path,
			Extensions:      ce.Extensions,
		}

		resp, err := s.OnTopicEvent(ctx, req)
		if err != nil {
			entries = append(entries, &pb.TopicEventBulkResponseEntry{
				EntryId: entry.EntryId,
				Status:  pb.TopicEventResponse_RETRY,
			})
			continue
		}

		entries = append(entries, &pb.TopicEventBulkResponseEntry{
			EntryId: entry.EntryId,
			Status:  resp.Status,
		})
	}

	return &pb.TopicEventBulkResponse{
		Statuses: entries,
	}, nil
}
