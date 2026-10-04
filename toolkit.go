// Package toolkit turns an operation registry into a complete tool binary:
// the generated CLI plus the built-in serve, mcp and metadata commands.
//
//	func main() {
//		reg := op.New("notes", version)
//		ops.Register(reg, backend)
//		toolkit.Main(reg, toolkit.Options{Description: "Notes."})
//	}
package toolkit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/0xble/toolkit/api"
	"github.com/0xble/toolkit/cli"
	"github.com/0xble/toolkit/mcp"
	"github.com/0xble/toolkit/op"
	"github.com/0xble/toolkit/output"
)

// Options configures a tool binary.
type Options struct {
	// Description is the root help text.
	Description string
	// Globals is an optional pointer to a struct of root flags; see
	// cli.Options.Globals.
	Globals any
	// Commands are hand-written commands mounted next to the operations.
	Commands []cli.Command
	// Authorizer decides whether a remote caller of serve (HTTP and HTTP MCP)
	// may run an operation. Nil means op.DenyWrites.
	Authorizer op.Authorizer
}

// Main runs the tool with os.Args and exits with the family exit code.
func Main(reg *op.Registry, opts Options) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := Run(ctx, reg, opts, os.Args[1:])
	stop()
	os.Exit(code)
}

// Run runs the tool with args and returns the exit code. Tests call it
// in-process.
func Run(ctx context.Context, reg *op.Registry, opts Options, args []string) int {
	return cli.Run(ctx, reg, CLIOptions(opts), args)
}

// CLIOptions is the cli configuration Main uses: the tool's own options plus
// the built-in serve, mcp and metadata commands.
func CLIOptions(opts Options) cli.Options {
	cmds := append([]cli.Command{
		{Name: "serve", Help: "Serve the operations over HTTP and MCP on a Unix socket", Cmd: &serveCmd{auth: opts.Authorizer}},
		{Name: "mcp", Help: "Serve MCP tools over stdio", Cmd: &mcpCmd{}},
		{Name: "metadata", Help: "Describe every operation (" + op.MetadataSchemaID + ")", Cmd: &metadataCmd{}},
	}, opts.Commands...)
	return cli.Options{Description: opts.Description, Globals: opts.Globals, Commands: cmds}
}

// Handler is the HTTP handler serve mounts on its socket: the api routes
// plus MCP at /mcp.
func Handler(reg *op.Registry, auth op.Authorizer) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", api.Handler(reg, api.Options{Authorizer: auth}))
	mux.Handle("/mcp", mcp.Handler(reg, mcp.Options{Authorizer: auth}))
	return mux
}

type serveCmd struct {
	Socket string `required:"" type:"path" help:"Unix socket path to listen on. Created with mode 0600."`

	auth op.Authorizer
}

func (c *serveCmd) Run(ctx *cli.Context) error {
	ln, err := ListenUnix(c.Socket)
	if err != nil {
		return op.Errorf(op.KindError, "listen_failed", "%v", err)
	}
	srv := &http.Server{Handler: Handler(ctx.Registry, c.auth), ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	_, _ = fmt.Fprintf(ctx.Stderr, "%s: serving on %s\n", ctx.Registry.Tool, c.Socket)
	select {
	case err := <-done:
		_ = os.Remove(c.Socket)
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdown)
	_ = os.Remove(c.Socket)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type mcpCmd struct{}

func (c *mcpCmd) Run(ctx *cli.Context) error {
	return mcp.ServeStdio(ctx, ctx.Registry, mcp.Options{})
}

type metadataCmd struct{}

func (c *metadataCmd) Run(ctx *cli.Context) error {
	m := ctx.Registry.Metadata()
	if ctx.JSON {
		return output.EncodeJSON(ctx.Stdout, m)
	}
	rows := make([][]string, 0, len(m.Operations))
	for _, o := range m.Operations {
		mcpName := "-"
		if o.MCP {
			mcpName = o.MCPName
		}
		rows = append(rows, []string{o.Name, string(o.Effect), o.CLI, mcpName, o.Summary})
	}
	output.PrintTable(ctx.Stdout, []string{"OPERATION", "EFFECT", "CLI", "MCP", "SUMMARY"}, rows)
	return nil
}
