package sender

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/vova4o/yandexadv/internal/agent/flags"
	"github.com/vova4o/yandexadv/internal/agent/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/vova4o/yandexadv/proto/metrics"
)

const (
	maxRetries = 3
	retryDelay = 1 * time.Second
)

func createGRPCClient(serverAddress string) (pb.MetricsServiceClient, *grpc.ClientConn, error) {
	conn, err := grpc.Dial(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	client := pb.NewMetricsServiceClient(conn)
	return client, conn, nil
}

// createTLSConfig creates TLS configuration with the provided certificate
func createTLSConfig(certPath string) (*tls.Config, error) {
	return &tls.Config{
		InsecureSkipVerify: true, // For development only
		MinVersion:         tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
	}, nil
}

// getProtocol returns http or https based on crypto path
func getProtocol(cryptoPath string) string {
	if cryptoPath != "" {
		return "https"
	}
	return "http"
}

// CompressData сжимает данные с использованием gzip
func CompressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	_, err := writer.Write(data)
	if err != nil {
		return nil, err
	}
	err = writer.Close()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ServerSupportsGzip проверяет, поддерживает ли сервер gzip-сжатие
func ServerSupportsGzip(cfg *flags.Config) bool {
	client := resty.New()
	protocol := getProtocol(cfg.CryptoPath)

	if cfg.CryptoPath != "" {
		tlsConfig, err := createTLSConfig(cfg.CryptoPath)
		if err != nil {
			log.Printf("Failed to create TLS config: %v", err)
			return false
		}
		client.SetTLSClientConfig(tlsConfig)
	}

	resp, err := client.R().
		SetHeader("Accept-Encoding", "gzip").
		Get(fmt.Sprintf("%s://%s", protocol, cfg.ServerAddress))
	if err != nil {
		log.Printf("Failed to check gzip support: %v\n", err)
		return false
	}

	return resp.Header().Get("Content-Encoding") == "gzip"
}

// calculateHash вычисляет HMAC-SHA256 хэш из данных и ключа
func calculateHash(data, key []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// getPublicIP получает публичный IP-адрес агента
func getPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(ip), nil
}

func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			continue
		}
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip.To4() != nil {
			return ip.String(), nil
		}
	}

	return "", fmt.Errorf("no local IP found")
}

// SendMetricsBatchGRPC отправляет метрики на сервер пакетом через gRPC
func SendMetricsBatchGRPC(cfg *flags.Config, metricsData []metrics.Metrics) {
	client, conn, err := createGRPCClient(cfg.ServerAddressGRPC)
	if err != nil {
		log.Printf("Failed to create gRPC client: %v", err)
		return
	}
	defer conn.Close()

	var grpcMetrics []*pb.MetricGRPC
    for _, m := range metricsData {
        grpcMetric := &pb.MetricGRPC{
            Id:   m.ID,
            Type: m.MType,
        }
        if m.Value != nil {
            grpcMetric.Value = *m.Value // Разыменовываем указатель
        }
        if m.Delta != nil {
            grpcMetric.Delta = *m.Delta // Разыменовываем указатель
        }
        grpcMetrics = append(grpcMetrics, grpcMetric)
    }

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = client.UpdateBatchMetrics(ctx, &pb.UpdateBatchMetricsRequest{Metrics: grpcMetrics})
	if err != nil {
		log.Printf("Failed to send metrics: %v", err)
	}
}

// SendMetricGRPC отправляет метрику на сервер через gRPC
func SendMetricGRPC(cfg *flags.Config, metric metrics.Metrics) {
	client, conn, err := createGRPCClient(cfg.ServerAddressGRPC)
	if err != nil {
		log.Printf("Failed to create gRPC client: %v", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	grpcMetric := &pb.MetricGRPC{
        Id:   metric.ID,
        Type: metric.MType,
    }
    if metric.Value != nil {
        grpcMetric.Value = *metric.Value // Разыменовываем указатель
    }
    if metric.Delta != nil {
        grpcMetric.Delta = *metric.Delta // Разыменовываем указатель
    }

    _, err = client.UpdateMetric(ctx, &pb.UpdateMetricRequest{Metric: grpcMetric})
    if err != nil {
        log.Printf("Failed to send metric: %v", err)
    }
}

// SendMetricsBatch отправляет метрики на сервер пакетом
func SendMetricsBatch(cfg *flags.Config, metricsData []metrics.Metrics) {
	client := resty.New()
	protocol := getProtocol(cfg.CryptoPath)

	// Configure TLS if crypto path is provided
	if cfg.CryptoPath != "" {
		tlsConfig, err := createTLSConfig(cfg.CryptoPath)
		if err != nil {
			log.Printf("Failed to create TLS config: %v", err)
			return
		}
		client.SetTLSClientConfig(tlsConfig)
	}

	url := fmt.Sprintf("%s://%s/updates", protocol, cfg.ServerAddress)
	log.Printf("Sending metrics to %s\n", url)
	useGzip := ServerSupportsGzip(cfg)

	// Сериализация метрик в JSON
	jsonData, err := json.Marshal(metricsData)
	if err != nil {
		log.Printf("Failed to marshal metrics: %v\n", err)
		return
	}

	var hash string
	if cfg.SecretKey != "" {
		hash = calculateHash(jsonData, []byte(cfg.SecretKey))
	}

	// Получение IP-адреса агента
	// Нам необходимо получать адрес агента на лету, так как в реальности мы не знаем где будет запущен агент
	agentIP, err := getPublicIP()
	if err != nil {
		log.Printf("Failed to get public IP: %v\n", err)
		agentIP, err = getLocalIP()
		if err != nil {
			log.Printf("Failed to get local IP: %v\n", err)
			return
		}
	}

	request := client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("HashSHA256", hash).
		SetHeader("X-Real-IP", agentIP)

	if useGzip {
		request.SetHeader("Content-Encoding", "gzip")
		compressedData, err := CompressData(jsonData)
		if err != nil {
			log.Printf("Failed to compress data for metrics: %v\n", err)
			return
		}
		request.SetBody(compressedData)
	} else {
		request.SetBody(jsonData)
	}

	if err := sendWithRetry(request, url); err != nil {
		log.Printf("Failed to send metrics: %v\n", err)
	}
}

// SendMetrics отправляет метрики на сервер
func SendMetrics(cfg *flags.Config, metricsData []metrics.Metrics) {
	client := resty.New()
	protocol := getProtocol(cfg.CryptoPath)

	if cfg.CryptoPath != "" {
		tlsConfig, err := createTLSConfig(cfg.CryptoPath)
		if err != nil {
			log.Printf("Failed to create TLS config: %v", err)
			return
		}
		client.SetTLSClientConfig(tlsConfig)
	}

	useGzip := ServerSupportsGzip(cfg)

	for _, metric := range metricsData {
		var url string
		if metric.Value == nil {
			url = fmt.Sprintf("%s://%s/update/%s/%s/%v", protocol, cfg.ServerAddress, metric.MType, metric.ID, *metric.Delta)
		} else {
			url = fmt.Sprintf("%s://%s/update/%s/%s/%v", protocol, cfg.ServerAddress, metric.MType, metric.ID, *metric.Value)
		}

		request := client.R().SetHeader("Content-Type", "text/plain")

		if useGzip {
			request.SetHeader("Content-Encoding", "gzip")
			compressedData, err := CompressData([]byte(url))
			if err != nil {
				log.Printf("Failed to compress data for metric %s: %v\n", metric.ID, err)
				continue
			}
			request.SetBody(compressedData)
		} else {
			request.SetBody(url)
		}

		if err := sendWithRetry(request, url); err != nil {
			log.Printf("Failed to send metric %s: %v\n", metric.ID, err)
		}
	}
}

// SendMetricsJSON отправляет метрики на сервер в формате JSON
func SendMetricsJSON(cfg *flags.Config, metricsData []metrics.Metrics) {
	client := resty.New()
	protocol := getProtocol(cfg.CryptoPath)

	if cfg.CryptoPath != "" {
		tlsConfig, err := createTLSConfig(cfg.CryptoPath)
		if err != nil {
			log.Printf("Failed to create TLS config: %v", err)
			return
		}
		client.SetTLSClientConfig(tlsConfig)
	}

	useGzip := ServerSupportsGzip(cfg)

	for _, metric := range metricsData {
		url := fmt.Sprintf("%s://%s/update/", protocol, cfg.ServerAddress)

		// Сериализация метрики в JSON
		jsonData, err := json.Marshal(metric)
		if err != nil {
			log.Printf("Failed to marshal metric %s: %v\n", metric.ID, err)
			continue
		}

		request := client.R().SetHeader("Content-Type", "application/json")

		if useGzip {
			request.SetHeader("Content-Encoding", "gzip")
			compressedData, err := CompressData(jsonData)
			if err != nil {
				log.Printf("Failed to compress data for metric %s: %v\n", metric.ID, err)
				continue
			}
			request.SetBody(compressedData)
		} else {
			request.SetBody(jsonData)
		}

		if err := sendWithRetry(request, url); err != nil {
			log.Printf("Failed to send metric %s: %v\n", metric.ID, err)
		}
	}
}

// sendWithRetry отправляет запрос с повторными попытками в случае ошибки
func sendWithRetry(request *resty.Request, url string) error {
	delay := retryDelay
	for i := 0; i < maxRetries; i++ {
		resp, err := request.Post(url)
		if err != nil {
			log.Printf("Failed to send request: %v\n", err)
		} else if resp.StatusCode() == 200 {
			return nil
		} else {
			log.Printf("Failed to send request: status code %d\n", resp.StatusCode())
			log.Printf("Response body: %s\n", resp.String())
		}

		time.Sleep(delay)
		delay += 2 * time.Second
	}
	return fmt.Errorf("failed to send request after %d attempts", maxRetries)
}
