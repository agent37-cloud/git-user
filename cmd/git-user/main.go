// main.go
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	_ "modernc.org/sqlite"
	"github.com/sahilm/fuzzy"
)

/* ---- data ---- */

type User struct {
	ID    int64
	Name  string
	Email string
}

type Store struct{ db *sql.DB }

func openStore() (*Store, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	// renamed tool: git-user
	dir := filepath.Join(cfgDir, "git-user")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "users.sqlite3")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
		PRAGMA journal_mode=WAL;
		CREATE TABLE IF NOT EXISTS users(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT NOT NULL,
			UNIQUE(name, email)
		);
	`); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) allUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, email FROM users ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
func (s *Store) insertUser(ctx context.Context, name, email string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO users(name,email) VALUES(?,?)`, strings.TrimSpace(name), strings.TrimSpace(email))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}
func (s *Store) deleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	return err
}

/* ---- TUI items ---- */

type userItem User

func (u userItem) Title() string       { return u.Name }
func (u userItem) Description() string { return u.Email }
func (u userItem) FilterValue() string { return u.Name + " <" + u.Email + ">" }

/* ---- fuzzy filter ---- */

type fuzzyList struct {
	all   []list.Item
	view  []list.Item
	query string
}

func (fl *fuzzyList) setAll(items []list.Item) {
	fl.all = items
	fl.apply("")
}
func (fl *fuzzyList) apply(q string) {
	fl.query = q
	if strings.TrimSpace(q) == "" {
		fl.view = fl.all
		return
	}
	type kv struct{ s string; it list.Item }
	var arr []kv
	for _, it := range fl.all {
		arr = append(arr, kv{it.FilterValue(), it})
	}
	var corpus []string
	for _, k := range arr {
		corpus = append(corpus, k.s)
	}
	matches := fuzzy.Find(q, corpus)
	fl.view = fl.view[:0]
	for _, m := range matches {
		fl.view = append(fl.view, arr[m.Index].it)
	}
}

/* ---- keymap ---- */

type keymap struct {
	Quit   key.Binding
	Add    key.Binding
	Edit   key.Binding
	Delete key.Binding
	Global key.Binding
	Local  key.Binding
	Filter key.Binding
	Clear  key.Binding
	Help   key.Binding
	Enter  key.Binding
}

func newKeymap() keymap {
	return keymap{
		Quit:   key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Add:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "add user")),
		Edit:   key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit (rename)")),
		Delete: key.NewBinding(key.WithKeys("del", "backspace"), key.WithHelp("del", "delete")),
		Global: key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "set global")),
		Local:  key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "set local")),
		Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Clear:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter / cancel")),
		Help:   key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "default action")),
	}
}

/* ---- styles ---- */

var (
	styles = struct {
		title   lipgloss.Style
		status  lipgloss.Style
		error   lipgloss.Style
		footer  lipgloss.Style
		focus   lipgloss.Style
		header  lipgloss.Style
	}{
		title:  lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true),
		status: lipgloss.NewStyle().Foreground(lipgloss.Color("86")),
		error:  lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true),
		footer: lipgloss.NewStyle().Faint(true),
		focus:  lipgloss.NewStyle().Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Bold(true),
		header: lipgloss.NewStyle().Foreground(lipgloss.Color("45")).Bold(true),
	}
)

/* ---- modes ---- */

type mode int

const (
	modeList mode = iota
	modeAdd
	modeEdit
)

/* ---- model ---- */

type model struct {
	store   *Store
	keys    keymap
	help    help.Model
	list    list.Model
	filter  textinput.Model
	form1   textinput.Model
	form2   textinput.Model
	fuzzy   fuzzyList
	mode    mode
	status  string
	errMsg  string
	width   int
	height  int
}

func initialModel(s *Store, users []User) model {
	items := make([]list.Item, 0, len(users))
	for _, u := range users {
		items = append(items, userItem(u))
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.Title = "Git Users"
	l.Styles.Title = styles.title
	fi := textinput.New()
	fi.Placeholder = "fuzzy filter (/, esc)"
	fi.Prompt = "  "
	fi.CharLimit = 256
	fi.Blur()
	n := textinput.New()
	n.Placeholder = "Full Name"
	n.Prompt = "Name: "
	e := textinput.New()
	e.Placeholder = "name@example.com"
	e.Prompt = "Email: "
	m := model{
		store:  s,
		keys:   newKeymap(),
		help:   help.New(),
		list:   l,
		filter: fi,
		form1:  n,
		form2:  e,
		mode:   modeList,
		status: "↑/↓ select, g=global, l=local, a=add, ?=help",
	}
	m.fuzzy.setAll(items)
	m.syncListView()
	return m
}

func (m *model) syncListView() {
	m.list.SetItems(m.fuzzy.view)
}

func (m model) Init() tea.Cmd { return nil }

/* ---- git ops ---- */

func gitInRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func setGitUser(scope string, u User) error {
	args := []string{"config"}
	if scope == "global" {
		args = append(args, "--global")
	}
	if out, err := exec.Command("git", append(args, "user.name", u.Name)...).CombinedOutput(); err != nil {
		return fmt.Errorf("git: %v: %s", err, string(out))
	}
	if out, err := exec.Command("git", append(args, "user.email", u.Email)...).CombinedOutput(); err != nil {
		return fmt.Errorf("git: %v: %s", err, string(out))
	}
	return nil
}

func readGitUser(scope string) (User, error) {
	args := []string{"config"}
	switch scope {
	case "global":
		args = append(args, "--global")
	case "local":
		// no flag needed; fail if not a repo
		if !gitInRepo() {
			return User{}, errors.New("not in a git repo")
		}
	}
	nameOut, nerr := exec.Command("git", append(args, "user.name")...).CombinedOutput()
	emailOut, eerr := exec.Command("git", append(args, "user.email")...).CombinedOutput()
	if nerr != nil || eerr != nil {
		return User{}, fmt.Errorf("no %s git user configured", scope)
	}
	return User{Name: strings.TrimSpace(string(nameOut)), Email: strings.TrimSpace(string(emailOut))}, nil
}

/* ---- update ---- */

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(m.width, m.height-7)
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case modeList:
			return m.updateListKeys(msg)
		case modeAdd, modeEdit:
			return m.updateFormKeys(msg)
		}
	}
	var cmd tea.Cmd
	switch m.mode {
	case modeList:
		m.list, cmd = m.list.Update(msg)
	case modeAdd, modeEdit:
		var c1, c2 tea.Cmd
		m.form1, c1 = m.form1.Update(msg)
		m.form2, c2 = m.form2.Update(msg)
		cmd = tea.Batch(c1, c2)
	}
	return m, cmd
}

func (m model) updateListKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if key.Matches(k, m.keys.Quit) {
		return m, tea.Quit
	}
	if key.Matches(k, m.keys.Help) {
		m.status = "keys: a add • g set global • l set local • / filter • del delete • e edit • enter=set local"
		return m, nil
	}
	if key.Matches(k, m.keys.Filter) {
		m.filter.SetValue("")
		m.filter.CursorStart()
		m.filter.Focus()
		m.mode = modeList
		m.status = "type to filter, enter to apply, esc to clear"
		return m, nil
	}
	if k.Type == tea.KeyRunes && m.filter.Focused() {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(k)
		m.fuzzy.apply(m.filter.Value())
		m.syncListView()
		return m, cmd
	}
	switch k.Type {
	case tea.KeyEnter:
		return m.applySelected("local")
	case tea.KeyEsc:
		if m.filter.Focused() {
			m.filter.Blur()
			m.filter.SetValue("")
			m.fuzzy.apply("")
			m.syncListView()
			m.status = "filter cleared"
			return m, nil
		}
	}
	if key.Matches(k, m.keys.Add) {
		m.mode = modeAdd
		m.form1.SetValue("")
		m.form2.SetValue("")
		m.form1.Focus()
		m.status = "add user: enter to next/save, esc to cancel"
		return m, nil
	}
	if key.Matches(k, m.keys.Delete) {
		it, ok := m.list.SelectedItem().(userItem)
		if !ok {
			return m, nil
		}
		_ = m.store.deleteUser(context.Background(), it.ID)
		users, _ := m.store.allUsers(context.Background())
		items := make([]list.Item, 0, len(users))
		for _, u := range users {
			items = append(items, userItem(u))
		}
		m.fuzzy.setAll(items)
		m.syncListView()
		m.status = "deleted"
		return m, nil
	}
	if key.Matches(k, m.keys.Edit) {
		it, ok := m.list.SelectedItem().(userItem)
		if !ok {
			return m, nil
		}
		m.mode = modeEdit
		m.form1.SetValue(it.Name)
		m.form2.SetValue(it.Email)
		m.form1.Focus()
		m.status = "edit user: enter cycles fields, esc cancels"
		return m, nil
	}
	if key.Matches(k, m.keys.Global) {
		return m.applySelected("global")
	}
	if key.Matches(k, m.keys.Local) {
		return m.applySelected("local")
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(k)
	return m, cmd
}

func (m model) updateFormKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(k, m.keys.Clear):
		m.mode = modeList
		m.status = "cancelled"
		return m, nil
	case k.Type == tea.KeyEnter:
		if m.form1.Focused() {
			m.form1.Blur()
			m.form2.Focus()
			return m, nil
		}
		name := strings.TrimSpace(m.form1.Value())
		email := strings.TrimSpace(m.form2.Value())
		if name == "" || email == "" || !strings.Contains(email, "@") {
			m.errMsg = "invalid name/email"
			return m, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if m.mode == modeAdd {
			if _, err := m.store.insertUser(ctx, name, email); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
		} else {
			if it, ok := m.list.SelectedItem().(userItem); ok {
				_ = m.store.deleteUser(ctx, it.ID)
			}
			if _, err := m.store.insertUser(ctx, name, email); err != nil {
				m.errMsg = err.Error()
				return m, nil
			}
		}
		users, _ := m.store.allUsers(context.Background())
		items := make([]list.Item, 0, len(users))
		for _, u := range users {
			items = append(items, userItem(u))
		}
		m.fuzzy.setAll(items)
		m.syncListView()
		m.mode = modeList
		m.status = "saved"
		return m, nil
	default:
		var c1, c2 tea.Cmd
		m.form1, c1 = m.form1.Update(k)
		m.form2, c2 = m.form2.Update(k)
		return m, tea.Batch(c1, c2)
	}
}

func (m model) applySelected(scope string) (tea.Model, tea.Cmd) {
	it, ok := m.list.SelectedItem().(userItem)
	if !ok {
		return m, nil
	}
	if scope == "local" && !gitInRepo() {
		m.errMsg = "not inside a git repo (local set aborted)"
		return m, nil
	}
	if err := setGitUser(scope, User(it)); err != nil {
		m.errMsg = err.Error()
	} else {
		m.status = fmt.Sprintf("set %s: %s <%s>", scope, it.Name, it.Email)
	}
	return m, nil
}

/* ---- view ---- */

func (m model) View() string {
	var b strings.Builder
	title := styles.header.Render("git-user") + "  " + styles.status.Render(m.status)
	if m.errMsg != "" {
		title += "  " + styles.error.Render("! " + m.errMsg)
	}
	b.WriteString(title + "\n\n")

	switch m.mode {
	case modeList:
		b.WriteString(m.filter.View() + "\n")
		b.WriteString(m.list.View() + "\n")
	case modeAdd, modeEdit:
		lbl := "Add User"
		if m.mode == modeEdit {
			lbl = "Edit User"
		}
		b.WriteString(styles.title.Render(lbl) + "\n\n")
		b.WriteString(m.form1.View() + "\n")
		b.WriteString(m.form2.View() + "\n\n")
	}

	b.WriteString(styles.footer.Render(m.helpView()))
	return b.String()
}

func (m model) helpView() string {
	if m.mode == modeList {
		return "keys: ↑/↓ move • enter/local: set local • g: set global • a: add • e: edit • del: delete • /: filter • esc: clear • q: quit"
	}
	return "enter: next/save • esc: cancel • q: quit"
}

/* ---- bootstrap current git config into DB ---- */

func hydrateFromGit(store *Store) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Always try to import global config (requested).
	if u, err := readGitUser("global"); err == nil && u.Name != "" && u.Email != "" {
		_, _ = store.insertUser(ctx, u.Name, u.Email)
	}

	// Optionally import local if we're in a repo.
	if gitInRepo() {
		if u, err := readGitUser("local"); err == nil && u.Name != "" && u.Email != "" {
			_, _ = store.insertUser(ctx, u.Name, u.Email)
		}
	}
}

/* ---- main ---- */

func main() {
	store, err := openStore()
	if err != nil {
		fmt.Println("db error:", err)
		os.Exit(1)
	}
	defer store.db.Close()

	// Seed DB from existing git configs (global + local if present).
	hydrateFromGit(store)

	users, err := store.allUsers(context.Background())
	if err != nil {
		fmt.Println("load error:", err)
		os.Exit(1)
	}
	if _, err := tea.NewProgram(initialModel(store, users), tea.WithAltScreen()).Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) {
		fmt.Println("tui error:", err)
		os.Exit(1)
	}
}
