// Command olivepress runs the OlivePress fresh-fruit intake gate HTTP service.
//
// It opens a SQLite WAL database, seeds the fictional grove catalogue, and
// serves the JSON HTTP interface. Restart recovery rebuilds open resource
// occupancy, task states and final barriers from persisted rows only.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/olivepress/fruit-intake-gate/api"
	"github.com/olivepress/fruit-intake-gate/catalog"
	"github.com/olivepress/fruit-intake-gate/store"
)

func main() {
	addr := flag.String("addr", envOr("OLIVEPRESS_ADDR", ":8080"), "HTTP listen address")
	dbPath := flag.String("db", envOr("OLIVEPRESS_DB", "olivepress.db"), "SQLite database path")
	flag.Parse()

	st, err := store.NewSQLite(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	if err := seedCatalog(st); err != nil {
		log.Fatalf("seed catalog: %v", err)
	}

	srv := api.NewServer(st)
	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("olivepress listening on %s (db=%s)", *addr, *dbPath)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = st.Close()
	log.Println("olivepress stopped")
}

// seedCatalog registers the fictional Picual grove catalogue in the store so
// the lock endpoint can validate cultivars, harvest windows and rule digests.
func seedCatalog(st store.Store) error {
	c := catalog.SeedCatalog()
	for _, p := range c.Plots() {
		if err := st.PutPlot(context.Background(), p); err != nil {
			return err
		}
	}
	for _, r := range c.Rules() {
		if err := st.PutRule(context.Background(), r); err != nil {
			return err
		}
	}
	return nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
