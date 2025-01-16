package handler

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/vova4o/yandexadv/internal/models"
)

// Router структура для роутера
type Router struct {
	Middl      Middlewarer   // middleware
	mux        *gin.Engine   // роутер
	Service    Servicer      // сервис
	server     *http.Server  // сервер
	stopCh     chan struct{} // канал для остановки сервера
	mu         sync.Mutex    // мьютекс
	cryptoPath string        // путь к сертификату
}

// Middlewarer интерфейс для middleware
type Middlewarer interface {
	GinZap() gin.HandlerFunc
	GunzipMiddleware() gin.HandlerFunc
	GzipMiddleware() gin.HandlerFunc
	CheckHash() gin.HandlerFunc
	CheckTrustedSubnet() gin.HandlerFunc
}

// Servicer интерфейс для сервиса
type Servicer interface {
	UpdateServ(metric models.Metric) error
	UpdateServJSON(metric *models.Metrics) error
	GetValueServ(metric models.Metrics) (string, error)
	GetValueServJSON(metric models.Metrics) (*models.Metrics, error)
	MetrixStatistic() (*template.Template, map[string]models.Metrics, error)
	UpdateBatchMetricsServ(metrics []models.Metrics) error
	PingDB() error
}

// New создание нового роутера
func New(s Servicer, middleware Middlewarer, path string) *Router {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	return &Router{
		Middl:      middleware,
		mux:        router,
		Service:    s,
		stopCh:     make(chan struct{}),
		cryptoPath: path,
	}
}

// RegisterRoutes регистрация маршрутов
func (s *Router) RegisterRoutes() {
	s.mux.Use(s.Middl.GinZap())
	s.mux.Use(s.Middl.GunzipMiddleware())
	s.mux.Use(s.Middl.GzipMiddleware())
	s.mux.Use(s.Middl.CheckTrustedSubnet())

	updatesGroup := s.mux.Group("/updates")
	updatesGroup.Use(s.Middl.CheckHash())
	{
		updatesGroup.POST("/", s.UpdateBatchMetricsHandler)
	}

	s.mux.POST("/update/:type/:name/:value", s.UpdateMetricHandler)
	// s.mux.POST("/updates/", s.UpdateBatchMetricsHandler)
	s.mux.GET("/value/:type/:name", s.GetValueHandler)
	s.mux.GET("/", s.StatisticPage)
	s.mux.POST("/update/", s.UpdateMetricHandlerJSON)
	s.mux.POST("/value/", s.GetValueHandlerJSON)
	s.mux.GET("/ping", s.PingHandler)
}

func (s *Router) getFilesFromPath() (string, string, error) {
	files, err := os.ReadDir(s.cryptoPath)
	if err != nil {
		return "", "", err
	}

	var cert, key string
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if file.Name() == "server.pem" {
			cert = s.cryptoPath + "/server.pem"
		}
		if file.Name() == "server.key" {
			key = s.cryptoPath + "/server.key"
		}
	}

	return cert, key, nil
}

// StartServer запуск сервера
func (s *Router) StartServer(addr string) error {
	// Создание http.Server с использованием Gin
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	if s.cryptoPath != "" {
		// Загрузка сертификата
		cert, key, err := s.getFilesFromPath()
		if err != nil {
			log.Println("failed to load cert", err)
		}

		if err := s.server.ListenAndServeTLS(cert, key); err != nil && err != http.ErrServerClosed {
			// Логирование ошибки, если сервер не смог запуститься
			log.Println("failed to start server", err)
			panic(err)
		}
	} else {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Логирование ошибки, если сервер не смог запуститься
			log.Println("failed to start server", err)
			panic(err)
		}
	}

	<-s.stopCh
	return nil
}

// StopServer остановка сервера
func (s *Router) StopServer(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	close(s.stopCh)
	// Остановка сервера с использованием контекста
	return s.server.Shutdown(ctx)
}
