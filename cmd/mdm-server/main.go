// mdm-server es el servidor de la plataforma MDM: API de agentes, API de la
// consola y la propia consola web.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"mdm/internal/protocol"
	"mdm/internal/server/alerts"
	"mdm/internal/server/androidsvc"
	"mdm/internal/server/app"
	"mdm/internal/server/auth"
	"mdm/internal/server/automations"
	"mdm/internal/server/files"
	"mdm/internal/server/jobs"
	"mdm/internal/server/metrics"
	"mdm/internal/server/notify"
	"mdm/internal/server/reports"
	"mdm/internal/server/secrets"
	"mdm/internal/server/settings"
	"mdm/internal/server/store"
	"mdm/web"
)

// version se fija al compilar con -ldflags "-X main.version=…".
var version = "dev"

const usage = `Uso:
  mdm-server serve                                    Arranca el servidor
  mdm-server admin create --email <correo> [--role admin|operator|readonly]
                                                      Crea un administrador
  mdm-server admin reset-totp --email <correo>        Borra el segundo factor de un administrador
  mdm-server version                                  Muestra la versión

Configuración (variables de entorno):
  MDM_DATABASE_URL    Cadena de conexión de PostgreSQL (obligatoria)
  MDM_PUBLIC_URL      Dirección pública, por ejemplo https://mdm.ejemplo.es (obligatoria)
  MDM_LISTEN_ADDR     Dirección de escucha (por defecto :8080)
  MDM_DOWNLOADS_DIR   Carpeta con los binarios del agente (por defecto /downloads)
  MDM_TRUSTED_PROXY   true si hay un proxy inverso delante
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 1
	}
	var err error
	switch {
	case args[0] == "serve":
		err = serve()
	case args[0] == "version":
		fmt.Fprintln(stdout, version)
	case args[0] == "admin" && len(args) > 1 && args[1] == "create":
		err = adminCreate(args[2:], stdin, stdout, stderr)
	case args[0] == "admin" && len(args) > 1 && args[1] == "reset-totp":
		err = adminResetTOTP(args[2:], stdout, stderr)
	case args[0] == "help" || args[0] == "-h" || args[0] == "--help":
		fmt.Fprint(stdout, usage)
	default:
		fmt.Fprintf(stderr, "Orden desconocida: %s\n\n%s", strings.Join(args, " "), usage)
		return 1
	}
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	return 0
}

func serve() error {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	cfg, err := app.LoadConfig(os.Getenv)
	if err != nil {
		return err
	}
	cfg.Version = version
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := store.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	s, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer s.Close()

	runner := jobs.New(s, time.Now, slog.Default())
	app.RegisterJobs(runner, s, files.New(cfg.FilesDir, 1<<30), time.Now)
	box, err := secrets.New(cfg.SecretKey)
	if err != nil {
		return err
	}
	app.RegisterAndroidJobs(runner, androidsvc.New(androidsvc.Deps{Store: s, Secrets: box, APIBase: cfg.AndroidAPIBase, Now: time.Now,
		CheckinIntervalS: protocol.DefaultCheckinIntervalS, Log: slog.Default()}))
	alertEngine := alerts.New(alerts.Deps{Store: s, Now: time.Now, PublicURL: cfg.PublicURL, Log: slog.Default()})
	deliverer := &notify.Deliverer{Store: s, Secrets: box, Version: version, Now: time.Now}
	location := func() *time.Location {
		v, err := settings.Load(ctx, s)
		if err != nil {
			return settings.Defaults().Location()
		}
		return v.Location()
	}
	autoEngine := automations.New(automations.Deps{Store: s, Secrets: box, Now: time.Now, Location: location, Log: slog.Default()})
	app.RegisterAlertJobs(runner, alertEngine, deliverer, autoEngine.OnEvent)
	app.RegisterAutomationJobs(runner, autoEngine, s, time.Now)
	app.RegisterMetricsJobs(runner, &metrics.Snapshotter{Store: s, Now: time.Now, Location: location, CheckinIntervalS: protocol.DefaultCheckinIntervalS})
	app.RegisterReportJobs(runner, &reports.Scheduler{Store: s, Now: time.Now, Location: location, PublicURL: cfg.PublicURL, Log: slog.Default()})
	go runner.Run(ctx)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           app.NewHandler(cfg, s, web.Dist(), time.Now),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	slog.Info("servidor en marcha", "addr", cfg.ListenAddr, "public_url", cfg.PublicURL, "version", version)

	select {
	case err := <-errCh:
		return fmt.Errorf("el servidor se detuvo: %w", err)
	case <-ctx.Done():
	}
	slog.Info("cerrando: se esperan hasta 15 s las peticiones en curso")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("cierre: %w", err)
	}
	return nil
}

// openStore abre el almacén para las órdenes de administración, aplicando
// antes las migraciones para que funcionen también en una instalación nueva.
func openStore(ctx context.Context) (*store.Store, error) {
	url := os.Getenv("MDM_DATABASE_URL")
	if url == "" {
		return nil, errors.New("falta MDM_DATABASE_URL: la cadena de conexión de PostgreSQL")
	}
	if err := store.Migrate(ctx, url); err != nil {
		return nil, err
	}
	return store.Open(ctx, url)
}

func adminCreate(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("admin create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := fs.String("email", "", "correo del administrador")
	roleFlag := fs.String("role", "admin", "rol: admin, operator o readonly")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !strings.Contains(*email, "@") {
		return errors.New("indica el correo con --email <correo>")
	}
	role, err := auth.ParseRole(*roleFlag)
	if err != nil {
		return errors.New("el rol debe ser admin, operator o readonly")
	}
	password, err := readNewPassword(stdin, stderr)
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	ctx := context.Background()
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()
	if _, err := s.CreateAdmin(ctx, strings.TrimSpace(*email), hash, string(role)); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return fmt.Errorf("ya existe un administrador con el correo %s", *email)
		}
		return err
	}
	fmt.Fprintf(stdout, "Administrador %s creado con rol %s. En su primer acceso configurará el segundo factor.\n", *email, role)
	return nil
}

// readNewPassword pide la contraseña dos veces sin eco cuando hay terminal.
// Sin terminal (por ejemplo, desde un script) lee una sola línea de la entrada.
func readNewPassword(stdin io.Reader, prompt io.Writer) (string, error) {
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(prompt, "Contraseña (mínimo 12 caracteres): ")
		first, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		fmt.Fprint(prompt, "Repite la contraseña: ")
		second, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		if string(first) != string(second) {
			return "", errors.New("las contraseñas no coinciden")
		}
		return string(first), nil
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		return "", errors.New("no se ha recibido la contraseña por la entrada estándar")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func adminResetTOTP(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("admin reset-totp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := fs.String("email", "", "correo del administrador")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("indica el correo con --email <correo>")
	}
	ctx := context.Background()
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()
	admin, err := s.AdminByEmail(ctx, *email)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no existe ningún administrador con el correo %s", *email)
	}
	if err != nil {
		return err
	}
	if err := s.SetAdminTOTP(ctx, admin.ID, nil); err != nil {
		return err
	}
	// Se cierran sus sesiones: quien pidió el restablecimiento debe volver a entrar.
	if err := s.DeleteSessionsForAdmin(ctx, admin.ID); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Segundo factor de %s borrado. Lo configurará de nuevo en su próximo acceso.\n", admin.Email)
	return nil
}
