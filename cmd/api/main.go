// Command api runs the DozLab API server.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dozlab-backend/internal/api"
	"dozlab-backend/internal/config"
	"dozlab-backend/internal/container"
	"dozlab-backend/internal/database"

	"github.com/gin-gonic/gin"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const shutdownTimeout = 15 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatalf("api: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.JWTSecret == "" {
		return errors.New("JWT_SECRET is required")
	}
	if cfg.RabbitMQ.URL == "" {
		return errors.New("RABBITMQ_URL is required")
	}

	db, err := database.Initialize(databaseURL(cfg.Database))
	if err != nil {
		return err
	}
	defer db.Close()

	c, err := container.NewContainer(cfg, db, k8sClient())
	if err != nil {
		return err
	}
	defer c.Close()

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	api.SetupRoutes(router, db, c.EventBus)

	srv := &http.Server{
		Addr:              ":" + cfg.ServerPort,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("api: listening on %s", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}

	log.Printf("api: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// databaseURL returns DATABASE_URL, or builds a Postgres URL from the DB_* settings.
func databaseURL(db config.DatabaseConfig) string {
	if db.URL != "" {
		return db.URL
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(db.User, db.Password),
		Host:     net.JoinHostPort(db.Host, db.Port),
		Path:     "/" + db.Name,
		RawQuery: "sslmode=" + getEnv("DB_SSLMODE", "disable"),
	}
	return u.String()
}

// k8sClient returns a dynamic client from the in-cluster config or the default
// kubeconfig, or nil if neither is available (lab session routes are then disabled).
func k8sClient() dynamic.Interface {
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
		if restCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig); err != nil {
			log.Printf("api: no Kubernetes config, continuing without it: %v", err)
			return nil
		}
	}
	client, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		log.Printf("api: Kubernetes client unavailable, continuing without it: %v", err)
		return nil
	}
	return client
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
