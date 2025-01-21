package handler

import (
	"context"

	"github.com/vova4o/yandexadv/internal/models"
	"github.com/vova4o/yandexadv/package/logger"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/vova4o/yandexadv/proto/metrics"
)

// GRPCServer структура для GRPC сервера
type GRPCServer struct {
	Service Servicer
	logger  *logger.Logger
	pb.UnimplementedMetricsServiceServer
}

// NewGRPCServer создание нового GRPC сервера
func NewGRPCServer(s Servicer, l *logger.Logger) *GRPCServer {
	return &GRPCServer{
		Service: s,
		logger:  l,
	}
}

// GetValue обработчик для получения значения метрики
func (s *GRPCServer) GetValue(ctx context.Context, req *pb.GetValueRequest) (*pb.GetValueResponse, error) {
	metrics := models.Metrics{
		MType: req.Type,
		ID:    req.Name,
	}

	value, err := s.Service.GetValueServ(metrics)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal server error")
	}

	return &pb.GetValueResponse{Value: value}, nil
}

// UpdateMetric обработчик для обновления метрики
func (s *GRPCServer) UpdateMetric(ctx context.Context, req *pb.UpdateMetricRequest) (*pb.UpdateMetricResponse, error) {
	metric := models.Metric{
		Name: req.Metric.Id,
		Type: req.Metric.Type,
	}

	if req.Metric.Delta != 0 {
		metric.Value = &req.Metric.Delta
	}

	if req.Metric.Value != 0 {
		metric.Value = &req.Metric.Value
	}

	err := s.Service.UpdateServ(metric)
	if err != nil {
		return nil, status.Error(codes.Internal, "internal server error")
	}

	return &pb.UpdateMetricResponse{}, nil
}

// Ping обработчик для проверки соединения с БД
func (s *GRPCServer) Ping(ctx context.Context, req *pb.PingRequest) (*pb.PingResponse, error) {
	err := s.Service.PingDB()
	if err != nil {
		return nil, status.Error(codes.Internal, "internal server error")
	}

	return &pb.PingResponse{Status: "pong"}, nil
}

// UpdateBatchMetrics обработчик для обновления метрик в формате GRPC by batch
func (s *GRPCServer) UpdateBatchMetrics(ctx context.Context, req *pb.UpdateBatchMetricsRequest) (*pb.UpdateBatchMetricsResponse, error) {
	metricsArr := convertProtoMetricsToModel(req.Metrics)
	if err := s.Service.UpdateBatchMetricsServ(metricsArr); err != nil {
		s.logger.Error("Failed to update metrics", zap.Error(err))
		return nil, status.Error(codes.Internal, "internal server error")
	}

	return &pb.UpdateBatchMetricsResponse{}, nil
}

// Service function to convert proto metrics to model metrics
func convertProtoMetricsToModel(protoMetrics []*pb.MetricGRPC) []models.Metrics {
	var metricsArr []models.Metrics
	for _, metric := range protoMetrics {
		var delta *int64
		if metric.Delta != 0 {
			delta = &metric.Delta
		}
		var value *float64
		if metric.Value != 0 {
			value = &metric.Value
		}
		metricsArr = append(metricsArr, models.Metrics{
			ID:    metric.Id,
			MType: metric.Type,
			Delta: delta,
			Value: value,
		})
	}
	return metricsArr
}
