// Package cli is magpie's command line: every command, and the app, tray
// and TUI it opens (the root's main.go only calls Main).
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/yetone/magpie/internal/agent"
	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/claudecode"
	"github.com/yetone/magpie/internal/codexrelay"
	"github.com/yetone/magpie/internal/edition"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/netproxy"
	"github.com/yetone/magpie/internal/notices"
	"github.com/yetone/magpie/internal/pluginrt"
	"github.com/yetone/magpie/internal/proc"
	"github.com/yetone/magpie/internal/profile"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/sessions"
	"github.com/yetone/magpie/internal/settings"
	"github.com/yetone/magpie/internal/shortcut"
	"github.com/yetone/magpie/internal/update"
)

// version is the root main package's, set at build time
// (-ldflags "-X main.version=…") and handed over by Main.
var version = "dev"

const usage = `Magpie Enterprise

  magpie serve [--web=host:port]   start the gateway and optional admin console
  magpie web --no-open             run the admin console in a browser
  magpie app                       open the employee desktop window
  magpie healthcheck               check the gateway listener
  magpie provider …                configure upstream providers
  magpie group …                   configure model routing and fallback
  magpie gateway-key …             issue, inspect, and revoke caller keys
  magpie usage …                   view gateway usage
  magpie settings …                inspect or change settings
  magpie claude|codex [model]       inspect or configure a local client
  magpie version|licenses          show the version or license notices

Run magpie <command> --help for command details.
`

var (
	bold  = lipgloss.NewStyle().Bold(true)
	muted = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#8B8F98", Dark: "#7C8290"})
	faint = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#C4C7CE", Dark: "#4A4F5A"})
	green = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#0F9D58", Dark: "#7EE2A8"})
)

// Main runs magpie with os.Args, at the version the build was stamped with.
func Main(v string) {
	version = v
	if err := edition.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "magpie:", err)
		os.Exit(1)
	}
	// before anything reads or writes a file: no home, or a relative one,
	// would put the agents' configs and magpie's keys under the working
	// folder. What needs no file still answers (magpie version in a
	// container or a script without HOME): run makes magpie's folders first.
	ignored, err := appdir.CheckEnv()
	if err != nil {
		if len(os.Args) > 1 {
			switch os.Args[1] {
			case "-v", "--version", "version":
				fmt.Println("magpie", version)
				return
			case "-h", "--help", "help":
				fmt.Print(usage)
				return
			case "licenses":
				fmt.Print(notices.Text)
				return
			}
		}
		fmt.Fprintln(os.Stderr, "magpie:", err)
		os.Exit(1)
	}
	// moved to a disk that isn't connected: never an empty magpie in its place
	unreachableStop(os.Args[1:])
	slices.Sort(ignored)
	for _, v := range ignored {
		fmt.Fprintf(os.Stderr, "magpie: ignoring %s: not an absolute path (the programs magpie starts still get it)\n", v)
	}
	if !edition.Enterprise() && provider.TookOpenedURL(os.Args[1:]) {
		// Claude Code, signing in for magpie, handed over the page to open
		return
	}
	endProbesOnSignal()
	gateway.Version = version
	netproxy.Install()
	update.GUI = hasGUI
	err = run(os.Args[1:])
	proc.EndProbes() // a CLI still being asked something isn't left to init
	if !edition.Enterprise() {
		sessions.Saved() // the session index kept, for the next run
	}
	if err != nil {
		// a command with exit codes of its own (quota wait) says which
		code := 1
		var e exitError
		if errors.As(err, &e) {
			code = e.code
		}
		if msg := err.Error(); msg != "" {
			fmt.Fprintln(os.Stderr, "magpie:", msg)
		}
		os.Exit(code)
	}
}

// syncAgentsOnChange sets what a catalog change (a provider or group
// saved, a vendor's list fetched anew) does: the model lists agents keep
// in files of their own are brought up to date behind the save, which
// answers without waiting for every agent's files to be written (on
// Windows, half a second for each one another program holds open).
func syncAgentsOnChange() { catalog.Changed = agent.SyncCatalogSoon }

// startUp brings the files magpie and its agents keep up to what this
// magpie reads, before a command that may read them; a var so a test can
// tell it ran.
var startUp = func() {
	// started by an update, the magpie it replaces goes first: the moves
	// below write the files it may still be writing
	update.AwaitPredecessor()
	makeDirs()
	settings.Migrate()
	// every built-in plugin starts on (plugins.yml), on a new install too
	if _, err := pluginrt.Settle(); err != nil {
		fmt.Fprintf(os.Stderr, "magpie: plugins.yml: %v\n", err)
	}
	if _, err := pluginrt.Refresh(); err != nil {
		fmt.Fprintf(os.Stderr, "magpie: plugins.yml: %v\n", err)
	}
	// a vendor's keys saved a provider each ("NVIDIA #001"…) are one
	// provider with those keys, before anything lists them
	if _, err := provider.FoldCopies(); err != nil {
		fmt.Fprintf(os.Stderr, "magpie: providers.json: %v\n", err)
	}
	// the providers and settings read once for every agent's fields, which
	// the moves below look at (a write among them reads them again)
	release := provider.Hold()
	if !edition.Enterprise() {
		agent.RenameLegacy()
		agent.MoveCursorEfforts()
		agent.MoveAntigravityEfforts()
		agent.MoveOffAccountIDs()
	}
	release()
}

func run(args []string) error {
	// a command that saved a provider or a group ends with the agents'
	// files written, which its save no longer waits for; and a test's run
	// leaves catalog.Changed as it found it
	was := catalog.Changed
	defer func() {
		agent.SyncCatalogSettled()
		catalog.Changed = was
	}()
	// before anything is read, moved or started: --help on any command
	// prints its usage and does nothing else
	if helpAsked(args) {
		fmt.Println(strings.TrimRight(helpFor(args), "\n"))
		return nil
	}
	if len(args) > 0 && args[0] == "healthcheck" {
		return healthcheck(args[1:]) // every few seconds in a container: nothing else
	}
	if len(args) > 0 && args[0] == "heap" {
		// the running magpie's heap profile: asks it, and moves nothing here
		return heapCmd(args[1:])
	}
	if len(args) > 0 && args[0] == "mtls" {
		// magpie's CA and the client certificates it issued: nothing
		// else is read or moved
		return mtlsCmd(args[1:])
	}
	if len(args) > 0 && args[0] == "https" {
		// HTTPS for other computers: the setting, the certificate and
		// magpie's CA, nothing else
		return httpsCmd(args[1:])
	}
	if len(args) > 0 && args[0] == "cpu" {
		// the running magpie's CPU profile: asks it, and moves nothing here
		return cpuCmd(args[1:])
	}
	if len(args) > 0 && args[0] == "goroutines" {
		// the running magpie's goroutines: asks it, and moves nothing here
		return goroutinesCmd(args[1:])
	}
	if len(args) > 0 && (args[0] == "log" || args[0] == "logs") {
		// where magpie's logs are, and the app log's last lines: reads
		// files, and moves nothing
		return logCmd(args[1:])
	}
	if len(args) == 2 && args[0] == agent.DryRunArg {
		// an agent disconnected on a copy of its files under a temporary
		// home, for the Agents page to show what disconnecting changes
		// (agent.DisconnectPreview)
		return agent.DryRun(args[1])
	}
	if len(args) > 0 {
		switch args[0] {
		case "statusline":
			return statuslineCmd(args)
		case codexrelay.Command: // internal: what a quitting magpie leaves for Codex's open threads
			return codexrelay.Main(args[1:], gateway.CodexRelay())
		case "data": // where magpie's files are, and moving them: before anything is made in them
			return dataCmd(args[1:])
		case "uninstall":
			// what the app starts as it quits from Settings › Uninstall
			// magpie: nothing is made again before its folders go
			if len(args) > 1 && args[1] == "--data-after" {
				return uninstallData(args[2:])
			}
		}
	}
	// magpie serve beside another magpie serving the gateway (the app, a
	// service, the copy an update left) waits for the port before it moves
	// or starts anything, rather than doing it all to fail to bind (#4938)
	if len(args) > 0 && args[0] == "serve" && plainServe(args[1:]) {
		awaitGatewayFree(gatewayFreeEvery)
	}
	startUp()
	// a provider added, edited or removed, or a list fetched anew, reaches
	// the model lists agents keep in files of their own, behind the save
	// that changed it (agent.SyncCatalogSoon)
	syncAgentsOnChange()
	// a model Claude Code names that magpie doesn't serve goes to the one
	// it is set to use for that tier
	gateway.StandIn = agent.StandIn
	// a bare name an agent asks for goes to its own subscription, unless
	// magpie set the agent to the group magpie found by that name
	gateway.SetToGroup = agent.SetToGroup
	// and its auto mode classifier, on a sonnet tier that follows a main
	// model /model has moved, to the model it moved to
	gateway.ClassifierStandIn = agent.ClassifierStandIn
	// a Codex that reaches magpie for account failover alone is handed
	// only its own models (#1385)
	gateway.CodexOwnOnly = agent.CodexOwnOnly
	// what magpie writes into an agent's TOML, YAML or dotenv config says
	// so in a comment, for whoever reads it once magpie is gone
	agent.MarkWritten()
	// the Codex app's titles, descriptions and summaries, which it asks
	// for on a model of its own, on the model of magpie's picked beside
	// its ChatGPT sign-in rather than on that account (atom_Ray on X)
	gateway.CodexSideModel = agent.CodexSideModel
	gateway.CodexModelNow = agent.CodexModelNow
	gateway.CodexSideSet = agent.CodexSideSet
	// the list the Codex app reads from a file beside the ChatGPT sign-in,
	// its picker showing magpie's models only so (laddieyao on Discord),
	// written again when the backend's list changes
	agent.CodexDesktopList = gateway.CodexDesktopCatalog
	gateway.CodexDesktopChanged = agent.SyncCatalogSoon
	if edition.Current() != edition.Server {
		// and dsh's patch lists, which dsh reads live: a route left behind by
		// something else writing the file fails every session there until
		// magpie writes its own list again
		gateway.WhileServing = append(gateway.WhileServing, agent.KeepDshWired)
		// and Cursor Private Inference's variables, which the Mac's launchd
		// forgets at a restart, for the gateway's address now
		gateway.WhileServing = append(gateway.WhileServing, agent.KeepCursorLocalEnv)
		// and magpie's models in OpenMausBot's engine, which OpenMausBot
		// writes back as it held them when an engine is changed in it
		gateway.WhileServing = append(gateway.WhileServing, agent.KeepOpenMausBotListed)
		// and Claude Science's daemon, which a restart of the computer ends,
		// started on the gateway again while it is connected
		gateway.WhileServing = append(gateway.WhileServing, agent.KeepClaudeScience)
	}
	if !edition.Enterprise() {
		// and every agent's own telemetry off while Settings › Privacy says so,
		// an agent found since too
		gateway.WhileServing = append(gateway.WhileServing, agent.KeepTelemetryAllOff)
		// and the Claude Code magpie downloaded for a machine with none (a
		// server's, a container's), at Claude Code's newest release: its runs
		// turn its own updater off
		gateway.WhileServing = append(gateway.WhileServing, claudecode.KeepCurrent)

	}
	if len(args) == 0 {
		if hasGUI && edition.Current() != edition.Server {
			return runGUI(true, "")
		}
		gateway.Serves = "serve"
		return serveCmd([]string{"--web"})
	}
	// a magpie:// link the system handed over (Windows, Linux): the app
	// opens it for the user to confirm
	if strings.HasPrefix(strings.ToLower(args[0]), "magpie:") {
		if !hasGUI {
			return importCmd(args)
		}
		return runGUI(false, args[0])
	}
	// how this process was started, for the Routing page of another magpie on
	// this computer to name the one that serves the gateway (#2606)
	switch args[0] {
	case "web", "serve", "tray":
		gateway.Serves = args[0]
	case "app", "gui", "panel", "-Embedding":
		gateway.Serves = "app"
	}
	switch args[0] {
	case "web":
		return webCmd(args[1:])
	case "app", "gui":
		// `magpie gui settings`: the window on that tab, as a restart to
		// update from it comes back (update.RelaunchArgs)
		if len(args) > 1 {
			return runWindow(args[1])
		}
		return runGUI(true, "")
	case "tray":
		return runGUI(false, "")
	case "-Embedding":
		// Windows starting magpie for a click on one of its notifications
		// (a usage alert, #368) left in the Action Center after it quit:
		// the window, on the Usage page
		return runWindow("usage")
	case "panel":
		return runPanel()
	case "autostart":
		return autostartCmd(args[1:])
	case "daemon":
		return daemonCmd(args[1:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	case "-v", "--version", "version":
		fmt.Println("magpie", version)
		return nil
	case "licenses":
		fmt.Print(notices.Text)
		return nil
	case "ls", "list":
		// in the order the app lists them; those hidden there come last, dimmed
		shown, hidden := settings.Arrange(settings.Load(), agent.Detected(), func(a *agent.Agent) string { return a.ID })
		return list(append(shown, hidden...), true, len(shown))
	case "agents":
		return list(agent.All(), false, -1)
	case "sync":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := catalog.Sync(ctx); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "catalog saved to", catalog.CachePath())
		refreshLive(ctx)
		return nil
	case "save", "use", "rm", "rename", "profiles":
		return profiles(args)
	case "import":
		return importCmd(args[1:])
	case "providers":
		return providers()
	case "presets":
		return presets()
	case "provider":
		return providerCmd(args)
	case "models":
		return models(args[1:])
	case "model":
		return modelCmd(args[1:])
	case "visible":
		return visibleCmd(args[1:])
	case "search":
		return searchCmd(args[1:])
	case "groups":
		return groups()
	case "group":
		return groupCmd(args)
	case "serve":
		return serveCmd(args[1:])
	case "gateway-key":
		return gatewayKeys(args)
	case "usage":
		return usageCmd(args)
	case "quota", "quotas":
		return quotaCmd(args)
	case "status":
		return statusCmd(args[1:])
	case "uninstall":
		// Windows' Installed apps: the app's window asks (shortcut.UninstallArgs)
		if hasGUI && slices.Equal(args, shortcut.UninstallArgs) {
			gateway.Serves = "app"
			return runUninstallWindow()
		}
		return uninstallCmd(args[1:])
	case "settings":
		return settingsCmd(args[1:])
	}

	// the agent of a plugin that is gone (deleted on disk) is no agent
	// here, but what magpie stashed of its file puts it back (#3810)
	if len(args) == 2 && strings.EqualFold(args[1], "default") {
		if g, ok, err := agent.RestoreGone(args[0]); ok {
			return goneDefault(args[0], g, err)
		}
	}
	a, err := agent.Find(args[0])
	if err != nil {
		return err
	}
	// no agent form takes a flag: one is never set as a model or a value
	// (`magpie snow --verbose` would take magpie out of Snow, #1457)
	if f := flagArg(args[1:]); f != "" {
		return fmt.Errorf("unknown flag %s: nothing was changed\n\n%s", f, strings.TrimRight(agentHelp(a), "\n"))
	}
	switch len(args) {
	case 1:
		return list([]*agent.Agent{a}, true, -1)
	case 2:
		// default is magpie's word in any case, as an agent's name and a
		// field's are: magpie codex DEFAULT wrote model = "DEFAULT"
		if strings.EqualFold(args[1], "default") {
			if a.Wired() {
				return disconnect(a)
			}
			return set(a, a.Fields[0].Key, "")
		}
		// `magpie codex model`: a field's name alone asks what it is set to,
		// where it was set as the model (model = "model"). Claude Code's
		// opus is a model's name as well as a tier's, and stays the model
		if f := a.Field(args[1]); f != nil && !modelAlias(a, args[1]) {
			return showField(a, f)
		}
		// `magpie codex xhigh`: a bare value that belongs to a non-model field
		// (effort levels, for instance) is routed there; anything else is a model.
		if f := fieldForValue(a, args[1]); f != nil {
			return set(a, f.Key, args[1])
		}
		return set(a, a.Fields[0].Key, args[1])
	case 3:
		value := args[2]
		if strings.EqualFold(value, "default") {
			value = ""
		}
		return set(a, args[1], value)
	}
	return fmt.Errorf("too many arguments\n\n%s", usage)
}

func set(a *agent.Agent, key, value string) error {
	f := a.Field(key)
	if f == nil {
		var keys []string
		for _, f := range a.Fields {
			keys = append(keys, f.Key)
		}
		return fmt.Errorf("%s has no field %q (fields: %s)", a.Name, key, strings.Join(keys, ", "))
	}
	// a field's name is no value: magpie codex subagent effort ran Codex's
	// subagents on a model called effort. Claude Code's opus is a model's
	// name as well as a tier's
	if g := a.Field(value); value != "" && g != nil && !modelAlias(a, value) {
		return fieldAsValue(a, f, g, value)
	}
	value, err := a.Spell(f.Key, value)
	if err != nil {
		return err
	}
	before := f.Get()
	wired := a.Wired()
	// a magpie model picked on an agent not connected yet connects it
	// first, as the Agents page's picker does (#1900): the agent is wired
	// in, and Disconnect puts back what it had before
	if err := a.Pick(f.Key, value); err != nil {
		return err
	}
	// what the config reads now, not what was asked: an agent may name the
	// model under a provider of its own (OpenCode's magpie-relay/…), and a
	// value it had already is said to be so
	now := f.Get()
	shown := now
	if value == "" || now == "" {
		shown = muted.Render("default")
	}
	if now == before {
		shown += " " + muted.Render("(unchanged)")
	}
	fmt.Println(green.Render("✓"), bold.Render(a.Name), muted.Render(f.Label), shown)
	// a value that is the agent's own, not one of magpie's, takes magpie
	// out: said, as Disconnect is, with the way back (#2000: magpie claude
	// sonnet put Claude Code back on its DeepSeek endpoint, which refused
	// Sonnet, and only "✓ Claude Code model sonnet" was printed)
	if wired && now != "" && !a.Wired() {
		fmt.Println(muted.Render(fmt.Sprintf("  disconnected from magpie: %s is %s's own, sent to the endpoint it had before magpie; magpie %s <model> with one of magpie's models connects it again", now, a.Name, a.ID)))
	}
	if a.Notice != nil && (!f.Live || !wired && a.Wired()) {
		if n := a.Notice(); n != "" {
			fmt.Println(muted.Render("  ↻ " + n))
		}
	}
	return nil
}

// modelAlias says whether a word names the agent's model, in any case,
// though a field goes by it too: Claude Code's opus, a tier's name as well
func modelAlias(a *agent.Agent, w string) bool {
	return slices.ContainsFunc(a.ModelAliases, func(m string) bool { return strings.EqualFold(m, w) })
}

// fieldAsValue refuses field g's name given as field f's value, for what
// was meant: two words that are one field's label (magpie codex subagent
// effort), a switch, which is turned on (Claude Code's ultracode), or the
// field, which its name alone shows.
func fieldAsValue(a *agent.Agent, f, g *agent.Field, value string) error {
	for _, name := range []string{f.Key + " " + value, f.Key + "_" + value} {
		if h := a.Field(name); h != nil {
			return fmt.Errorf("%q is one field of %s's: magpie %s %s shows it", h.Label, a.Name, a.ID, h.Key)
		}
	}
	if g.Options != nil {
		if o := g.Options(a.Values()); len(o) == 1 && o[0].Value == "on" {
			return fmt.Errorf("%q is a switch of %s's, not a value for its %s: magpie %s %s on", value, a.Name, f.Label, a.ID, g.Key)
		}
	}
	return fmt.Errorf("%q is a field of %s's, not a value for its %s: magpie %s %s shows it", value, a.Name, f.Label, a.ID, g.Key)
}

// showField is `magpie <agent> <field>`: what the field is set to, as set
// prints it
func showField(a *agent.Agent, f *agent.Field) error {
	v := f.Get()
	if v == "" {
		v = muted.Render("default")
	}
	fmt.Println(bold.Render(a.Name), muted.Render(f.Label), v)
	return nil
}

// disconnect is `magpie <agent> default` on an agent magpie is wired into:
// the Agents page's Disconnect, which puts back what the user had before
// magpie — Claude Code's own model, its endpoint — where a field's default
// leaves the agent as installed (__jingling on X: magpie claude default
// took the model they had set away with magpie's)
func disconnect(a *agent.Agent) error {
	before := a.Values()
	if err := a.Disconnect(); err != nil {
		return err
	}
	now := a.Values()
	for _, f := range a.Fields {
		if before[f.Key] == now[f.Key] {
			continue
		}
		shown := now[f.Key]
		if shown == "" {
			shown = muted.Render("default")
		}
		fmt.Println(green.Render("✓"), bold.Render(a.Name), muted.Render(f.Label), shown)
	}
	fmt.Println(muted.Render("  disconnected from magpie, back to what it had before"))
	if a.Notice != nil {
		if n := a.Notice(); n != "" {
			fmt.Println(muted.Render("  ↻ " + n))
		}
	}
	return nil
}

// goneDefault says what `magpie <id> default` did for the agent of a
// plugin that is gone.
func goneDefault(id string, g agent.Gone, err error) error {
	for _, p := range g.Put {
		fmt.Println(green.Render("✓"), bold.Render(id), muted.Render(p))
	}
	for _, p := range g.Off {
		fmt.Println(green.Render("✓"), bold.Render(id), muted.Render(p+": magpie was out of it already"))
	}
	if len(g.Put) > 0 {
		fmt.Println(muted.Render("  its plugin is gone; disconnected from magpie, back to what it had before"))
	}
	if g.PluginOff != "" {
		fmt.Println(green.Render("✓"), bold.Render(g.PluginOff), muted.Render("is not on magpie: its plugin is off, and magpie put back what it had when the plugin was turned off"))
	}
	if err != nil {
		return fmt.Errorf("%s's plugin is gone, and magpie couldn't put its config back: %w", id, err)
	}
	return nil
}

func fieldForValue(a *agent.Agent, v string) *agent.Field {
	vals := a.Values()
	// the agent's suffix after a model (omp's ":max") aside: a role on the
	// same model at that level offers it as typed, and would take it. A list
	// of models no picker offers: it is for the model
	if a.SplitSuffix != nil {
		m, _, one := a.SplitSuffix(v)
		if !one {
			return nil
		}
		v = m
	}
	// a model stays with the model, even where other fields offer it too
	// (Claude Code's opus/sonnet/haiku/fable)
	for _, o := range a.Fields[0].Options(vals) {
		if o.Value == v {
			return nil
		}
	}
	for i := 1; i < len(a.Fields); i++ {
		for _, o := range a.Fields[i].Options(vals) {
			if o.Value == v {
				return &a.Fields[i]
			}
		}
	}
	return nil
}

// list prints the agents; those from dimFrom on (when not -1) are the ones
// hidden in the app, and are dimmed.
func list(agents []*agent.Agent, detectedOnly bool, dimFrom int) error {
	if len(agents) == 0 {
		return fmt.Errorf("no supported agents found on this machine")
	}
	type row struct{ name, vals, path string }
	var rows []row
	nameW, valW := 0, 0
	for i, a := range agents {
		r := row{name: a.Name, path: tilde(a.Path)}
		if !detectedOnly && !a.Detected() {
			r.name = faint.Render(a.Name)
			r.vals = faint.Render("not detected")
			r.path = ""
		} else if a.CLIMissing() {
			// its folder left by an uninstall (#2173): what it holds is
			// read by nothing
			r.name = faint.Render(a.Name)
			r.vals = faint.Render("CLI not found")
		} else {
			vals := a.Values()
			dim := dimFrom >= 0 && i >= dimFrom
			label, value := muted, lipgloss.NewStyle()
			if dim {
				label, value = faint, faint
			}
			var parts []string
			for _, f := range a.Fields {
				v := vals[f.Key]
				if v == "" && f.Quiet {
					continue
				}
				if v == "" {
					v = faint.Render("—")
				} else {
					v = value.Render(v)
				}
				if f.Label == "model" {
					parts = append(parts, v)
				} else {
					parts = append(parts, label.Render(f.Label)+" "+v)
				}
			}
			r.name = bold.Render(a.Name)
			if dim {
				r.name = faint.Render(a.Name) + " " + faint.Render("hidden")
			}
			r.vals = strings.Join(parts, label.Render("  ·  "))
			// through magpie for account failover while not connected (#1385)
			if said := a.FailoverSaid(); said != "" {
				r.vals += label.Render("  ·  " + said)
			}
			if a.Import != nil {
				// unknown reads as not added here: the line only offers the
				// command, never says magpie is missing
				added := false
				if a.Added != nil {
					added, _ = a.Added()
				}
				if added {
					r.vals = value.Render("magpie added")
				} else {
					r.vals = label.Render("magpie "+a.ID+" add") + faint.Render("  to add magpie")
				}
			}
		}
		nameW = max(nameW, lipgloss.Width(r.name))
		valW = max(valW, lipgloss.Width(r.vals))
		rows = append(rows, r)
	}
	for _, r := range rows {
		fmt.Printf("  %s  %s  %s\n", pad(r.name, nameW), pad(r.vals, valW), faint.Render(r.path))
	}
	return nil
}

func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

func profiles(args []string) error {
	switch args[0] {
	case "profiles":
		ps, err := profile.Load()
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			fmt.Println(muted.Render("no profiles yet · magpie save <name>"))
			return nil
		}
		detected := agent.Detected()
		for _, n := range profile.Names(ps) {
			// ✓ the one the setup is now (superhtoooth on X)
			mark := " "
			if profile.InUse(ps[n], detected, nil) {
				mark = green.Render("✓")
			}
			fmt.Printf("%s %s  %s\n", mark, bold.Render(n), muted.Render(profile.LongSummary(ps[n])))
		}
		return nil
	case "save":
		if len(args) < 2 {
			return fmt.Errorf("usage: magpie save <name>")
		}
		p, err := profile.Snapshot()
		if err != nil {
			return err
		}
		if err := profile.Save(args[1], p); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "saved profile", bold.Render(args[1]))
		if p.Library != nil {
			fmt.Println(" ", muted.Render("with the "+p.Library.Summary()))
		}
		return nil
	case "use":
		if len(args) < 2 {
			return fmt.Errorf("usage: magpie use <name>")
		}
		ps, err := profile.Load()
		if err != nil {
			return err
		}
		p, ok := ps[args[1]]
		if !ok {
			return fmt.Errorf("no profile named %q", args[1])
		}
		a, err := profile.Apply(p)
		if err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "applied", bold.Render(args[1]), muted.Render(fmt.Sprintf("(%d changed)", a.Changed)))
		for _, line := range profile.Report(a) {
			fmt.Println(" ", muted.Render(line))
		}
		return nil
	case "rm":
		if len(args) < 2 {
			return fmt.Errorf("usage: magpie rm <name>")
		}
		if err := profile.Delete(args[1]); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "deleted profile", bold.Render(args[1]))
		return nil
	case "rename":
		// a flag is never taken for a name to write (magpie snow --help
		// wrote "--help" into a profile)
		if len(args) != 3 || strings.HasPrefix(args[2], "-") {
			return fmt.Errorf("usage: magpie rename <old> <new>")
		}
		if err := profile.Rename(args[1], args[2]); err != nil {
			return err
		}
		fmt.Println(green.Render("✓"), "renamed profile", bold.Render(args[1]), "to", bold.Render(args[2]))
		return nil
	}
	return nil
}

func pad(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}
