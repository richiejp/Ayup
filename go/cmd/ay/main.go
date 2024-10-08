package ay

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/pprof"

	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"

	"github.com/alecthomas/kong"
	"github.com/charmbracelet/lipgloss"
	"github.com/joho/godotenv"
	"github.com/muesli/termenv"

	"premai.io/Ayup/go/cli/key"
	"premai.io/Ayup/go/cli/login"
	"premai.io/Ayup/go/cli/push"
	"premai.io/Ayup/go/internal/terror"
	ayTrace "premai.io/Ayup/go/internal/trace"
	"premai.io/Ayup/go/internal/tui"
)

type Globals struct {
	Ctx    context.Context
	Tracer trace.Tracer
	Logger *slog.Logger
}

type PushCmd struct {
	Path      string `arg:"" optional:"" name:"path" help:"Path to the source code to be pushed" type:"path"`
	Assistant string `env:"AYUP_ASSISTANT_PATH" help:"The location of the assistant plugin source if any" type:"path"`

	Host       string `env:"AYUP_PUSH_HOST" default:"localhost:50051" help:"The location of a service we can push to"`
	P2pPrivKey string `env:"AYUP_CLIENT_P2P_PRIV_KEY" help:"Secret encryption key produced by 'ay key new'"`
}

func (s *PushCmd) Run(g Globals) (err error) {
	pprof.Do(g.Ctx, pprof.Labels("command", "push"), func(ctx context.Context) {
		if s.Path == "" {
			s.Path, err = os.Getwd()
			if err != nil {
				err = terror.Errorf(ctx, "getwd: %w", err)
				return
			}
		}

		p := push.Pusher{
			Tracer:       g.Tracer,
			Host:         s.Host,
			P2pPrivKey:   s.P2pPrivKey,
			AssistantDir: s.Assistant,
			SrcDir:       s.Path,
		}

		err = p.Run(pprof.WithLabels(g.Ctx, pprof.Labels("command", "push")))
	})

	return
}

type LoginCmd struct {
	Host       string `arg:"" env:"AYUP_LOGIN_HOST" help:"The server's P2P multi-address including the peer ID e.g. /dns4/example.com/50051/p2p/1..."`
	P2pPrivKey string `env:"AYUP_CLIENT_P2P_PRIV_KEY" help:"The client's private key, generated automatically if not set, also see 'ay key new'"`
}

func (s *LoginCmd) Run(g Globals) error {
	l := login.Login{
		Host:       s.Host,
		P2pPrivKey: s.P2pPrivKey,
	}

	return l.Run(g.Ctx)
}

type KeyNewCmd struct{}

func (s *KeyNewCmd) Run(g Globals) error {
	return key.New(g.Ctx)
}

var cli struct {
	Push  PushCmd  `cmd:"" help:"Figure out how to deploy your application"`
	Login LoginCmd `cmd:"" help:"Login to the Ayup service"`

	Daemon struct {
		Start           DaemonStartCmd           `cmd:"" help:"Start an Ayup service Daemon"`
		StartInRootless DaemonStartInRootlessCmd `cmd:"" passthrough:"" help:"Start a utility daemon to do tasks such as port forwarding in the Rootlesskit namesapce" hidden:""`
	} `cmd:"" help:"Self host Ayup on Linux"`

	Key struct {
		New KeyNewCmd `cmd:"" help:"Create a new private key"`
	} `cmd:"" help:"Manage encryption keys used by Ayup"`

	// maybe effected by https://github.com/open-telemetry/opentelemetry-go/issues/5562
	// also https://github.com/moby/moby/issues/46129#issuecomment-2016552967
	TelemetryEndpoint       string `group:"monitoring" env:"OTEL_EXPORTER_OTLP_ENDPOINT" help:"the host that telemetry data is sent to; e.g. http://localhost:4317"`
	TelemetryEndpointTraces string `group:"monitoring" env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT" help:"the host that traces data is sent to http://localhost:4317"`
	ProfilingEndpoint       string `group:"monitoring" env:"PYROSCOPE_ADHOC_SERVER_ADDRESS" help:"URL performance data is sent to; e.g. http://localhost:4040"`
}

func Main(version string) {
	ctx := context.Background()

	// Disable dynamic dark background detection
	// https://github.com/charmbracelet/lipgloss/issues/73
	lipgloss.SetHasDarkBackground(termenv.HasDarkBackground())
	titleStyle := tui.TitleStyle
	versionStyle := tui.VersionStyle
	errorStyle := tui.ErrorStyle
	fmt.Print(titleStyle.Render("Ayup!"), " ", versionStyle.Render("v"+version), "\n\n")

	confDir, userConfDirErr := os.UserConfigDir()
	var godotenvLoadErr error
	if userConfDirErr == nil {
		godotenvLoadErr = godotenv.Load(filepath.Join(confDir, "ayup", "env"))
	}

	ktx := kong.Parse(&cli, kong.UsageOnError(), kong.Description("Just make it run!"))

	ayTrace.SetupPyroscopeProfiling(cli.ProfilingEndpoint)

	if cli.TelemetryEndpoint != "" || cli.TelemetryEndpointTraces != "" {
		stopTracing, err := ayTrace.SetupOTelSDK(ctx)
		if err != nil {
			log.Fatalln(err)
		}
		defer func() {
			if err := stopTracing(ctx); err != nil {
				log.Fatalln(err)
			}
		}()
	}

	tracer := otel.Tracer("premai.io/Ayup/go/internal/trace")
	logger := otelslog.NewLogger("premai.io/Ayup/go/internal/trace")

	ctx = ayTrace.SetSpanKind(ctx, trace.SpanKindClient)
	ctx, span := tracer.Start(ctx, "main")
	defer span.End()

	terror.Ackf(ctx, "os UserConfigDir: %w", userConfDirErr)
	terror.Ackf(ctx, "godotenv load: %w", godotenvLoadErr)

	err := ktx.Run(Globals{
		Ctx:    ctx,
		Tracer: tracer,
		Logger: logger,
	})

	if err == nil {
		return
	}

	fmt.Println(errorStyle.Render("Error!"), err)

	var perr *kong.ParseError
	if errors.As(err, &perr) {
		if err := ktx.PrintUsage(false); err != nil {
			_ = terror.Errorf(ctx, "printusage: %w", err)
		}
	}
}
