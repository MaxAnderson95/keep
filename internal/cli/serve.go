package cli

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/urfave/cli/v2"

	"github.com/MaxAnderson95/keep/internal/serve"
	"github.com/MaxAnderson95/keep/web"
)

// rotateEvery is how often the resident serve process performs the same
// opportunistic log rotation a CLI invocation would (D23) — without it, logs
// would only rotate when some other keep command happens to run.
const rotateEvery = time.Hour

func cmdServe(bi BuildInfo) *cli.Command {
	return &cli.Command{
		Name:  "serve",
		Usage: "run the web management UI and JSON API",
		Description: "Serves the keep web UI and the /api/v1 API. Authentication comes from the\n" +
			"environment: KEEP_SERVE_PASSWORD (browser login, passkeys after that) and/or\n" +
			"KEEP_SERVE_TOKEN (Authorization: Bearer for scripting). Declared as a keep\n" +
			"Service, those normally arrive via env_files (ADR-0005).",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "host", Value: "127.0.0.1", Usage: "address to listen on"},
			&cli.IntFlag{Name: "port", Value: 4098, Usage: "port to listen on"},
		},
		Action: func(c *cli.Context) error {
			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
			srv, err := serve.New(serve.Options{
				ConfigPath:  configPath(c),
				Version:     bi.Version,
				Commit:      bi.Commit,
				Host:        c.String("host"),
				Port:        c.Int("port"),
				Password:    os.Getenv("KEEP_SERVE_PASSWORD"),
				Token:       os.Getenv("KEEP_SERVE_TOKEN"),
				SelfService: os.Getenv("KEEP_SERVICE"),
				StaticFS:    web.DistFS(),
				Logger:      logger,
			})
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go rotateLoop(ctx, c, bi, logger)
			return srv.ListenAndServe(ctx)
		},
	}
}

func rotateLoop(ctx context.Context, c *cli.Context, bi BuildInfo, logger *slog.Logger) {
	ticker := time.NewTicker(rotateEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// manager() loads the Config fresh and rotates opportunistically;
			// a broken mid-edit Config just skips this round.
			if _, err := manager(c, bi); err != nil {
				logger.Warn("rotation skipped", "error", err)
			}
		}
	}
}
