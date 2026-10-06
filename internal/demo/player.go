package demo

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultSpeed       = 20
	minTickWait        = time.Millisecond
	goAgain            = time.Duration(0)
	untilControlChange = time.Duration(-1)
)

//go:embed devstrip.tmpl
var devStripSource string

var devStrip = template.Must(template.New("devstrip").Parse(devStripSource))

type playback struct {
	paused  bool
	speed   float64
	until   time.Duration
	jumpTo  time.Duration
	restart bool
	tickNow bool
}

type Status struct {
	Paused  bool
	Speed   float64
	Elapsed time.Duration
	PRs     []PRSummary
	Error   string
}

type Player struct {
	scenario Scenario

	simMu sync.RWMutex
	sim   *Sim

	ctlMu sync.Mutex
	ctl   playback
	wake  chan struct{}
}

func NewPlayer(ctx context.Context, sc Scenario, speed float64, until time.Duration) (*Player, error) {
	if speed <= 0 {
		speed = sc.Speed
	}
	if speed <= 0 {
		speed = defaultSpeed
	}
	sim, err := NewSim(ctx, sc)
	if err != nil {
		return nil, err
	}
	return &Player{
		scenario: sc, sim: sim, wake: make(chan struct{}, 1),
		ctl: playback{speed: speed, until: until},
	}, nil
}

func (p *Player) current() *Sim {
	p.simMu.RLock()
	defer p.simMu.RUnlock()
	return p.sim
}

func (p *Player) playback() playback {
	p.ctlMu.Lock()
	defer p.ctlMu.Unlock()
	return p.ctl
}

func (p *Player) Close() error { return p.current().Close() }

func (p *Player) control(change func(*playback)) {
	p.ctlMu.Lock()
	change(&p.ctl)
	p.ctlMu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Player) Pause()  { p.control(func(c *playback) { c.paused = true }) }
func (p *Player) Resume() { p.control(func(c *playback) { c.paused, c.until = false, 0 }) }

func (p *Player) Restart() { p.control(func(c *playback) { c.restart = true }) }

func (p *Player) JumpTo(to time.Duration) { p.control(func(c *playback) { c.jumpTo = to }) }

func (p *Player) SetSpeed(speed float64) error {
	if !(speed > 0) || math.IsInf(speed, 0) {
		return fmt.Errorf("speed %v must be positive and finite", speed)
	}
	p.control(func(c *playback) { c.speed = speed })
	return nil
}

func (p *Player) MergeNow(number int) error {
	if err := p.current().forge.MergeNow(number); err != nil {
		return err
	}
	p.control(func(c *playback) { c.tickNow = true })
	return nil
}

func (p *Player) Status() Status {
	ctl := p.playback()
	sim := p.current()
	return Status{
		Paused:  ctl.paused,
		Speed:   ctl.speed,
		Elapsed: sim.Elapsed().Round(time.Second),
		PRs:     sim.forge.OpenPRs(),
	}
}

func (p *Player) step(ctx context.Context) (time.Duration, error) {
	ctl := p.playback()
	sim := p.current()

	switch {
	case ctl.restart:
		return goAgain, p.restart(ctx)
	case ctl.jumpTo > sim.Elapsed():
		return goAgain, sim.Tick(ctx)
	case ctl.jumpTo > 0:
		p.control(func(c *playback) { c.jumpTo = 0 })
		return goAgain, nil
	case ctl.tickNow:
		p.control(func(c *playback) { c.tickNow = false })
		return goAgain, sim.Tick(ctx)
	case ctl.paused:
		return untilControlChange, nil
	case ctl.until > 0 && sim.Elapsed() >= ctl.until:
		p.Pause()
		return goAgain, nil
	}
	if err := sim.Tick(ctx); err != nil {
		return goAgain, err
	}
	return max(time.Duration(float64(tickPeriod)/ctl.speed), minTickWait), nil
}

func (p *Player) restart(ctx context.Context) error {
	sim, err := NewSim(ctx, p.scenario)
	if err != nil {
		return err
	}
	p.simMu.Lock()
	old := p.sim
	p.sim = sim
	p.simMu.Unlock()
	if err := old.Close(); err != nil {
		return err
	}
	p.control(func(c *playback) { c.restart = false })
	return nil
}

func (p *Player) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		wait, err := p.step(ctx)
		if err != nil {
			return err
		}
		if wait != goAgain {
			p.sleep(ctx, wait)
		}
	}
	return nil
}

func (p *Player) sleep(ctx context.Context, d time.Duration) {
	var due <-chan time.Time
	if d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		due = timer.C
	}
	select {
	case <-ctx.Done():
	case <-p.wake:
	case <-due:
	}
}

func (p *Player) Serve(ctx context.Context, addr string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := &http.Server{
		Addr:              addr,
		Handler:           p.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errs := make(chan error, 2)
	go func() { errs <- p.Run(ctx) }()
	go func() {
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errs <- err
	}()

	first := <-errs
	cancel()
	shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	return errors.Join(first, srv.Shutdown(shutdown), <-errs)
}

func (p *Player) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", p.withStrip(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.current().server.ServeHTTP(w, r)
	})))
	mux.HandleFunc("GET /dev/state", func(w http.ResponseWriter, _ *http.Request) { p.render(w, "state", "") })
	mux.HandleFunc("POST /dev/pause", p.act(func(*http.Request) error { p.Pause(); return nil }))
	mux.HandleFunc("POST /dev/resume", p.act(func(*http.Request) error { p.Resume(); return nil }))
	mux.HandleFunc("POST /dev/restart", p.act(func(*http.Request) error { p.Restart(); return nil }))
	mux.HandleFunc("POST /dev/speed", p.act(func(r *http.Request) error {
		speed, err := strconv.ParseFloat(r.FormValue("speed"), 64)
		if err != nil {
			return err
		}
		return p.SetSpeed(speed)
	}))
	mux.HandleFunc("POST /dev/jump", p.act(func(r *http.Request) error {
		to, err := time.ParseDuration(r.FormValue("to"))
		if err != nil {
			return err
		}
		p.JumpTo(to)
		return nil
	}))
	mux.HandleFunc("POST /dev/merge-now", p.act(func(r *http.Request) error {
		number, err := strconv.Atoi(r.FormValue("number"))
		if err != nil {
			return err
		}
		return p.MergeNow(number)
	}))
	return mux
}

func (p *Player) act(do func(*http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var problem string
		if err := do(r); err != nil {
			problem = err.Error()
		}
		p.render(w, "state", problem)
	}
}

func (p *Player) render(w http.ResponseWriter, name, problem string) {
	html, err := p.execute(name, problem)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func (p *Player) execute(name, problem string) (string, error) {
	status := p.Status()
	status.Error = problem
	var out strings.Builder
	err := devStrip.ExecuteTemplate(&out, name, status)
	return out.String(), err
}

func (p *Player) withStrip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isPage := r.Method == http.MethodGet && r.Header.Get("HX-Request") == "" &&
			strings.Contains(r.Header.Get("Accept"), "text/html")
		if !isPage {
			next.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		body := rec.Body.String()
		if strings.Contains(rec.Header().Get("Content-Type"), "text/html") && strings.Contains(body, "</body>") {
			strip, err := p.execute("strip", "")
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			body = strings.Replace(body, "</body>", strip+"</body>", 1)
		}
		for key, values := range rec.Header() {
			w.Header()[key] = values
		}
		w.Header().Del("Content-Length")
		w.WriteHeader(rec.Code)
		_, _ = w.Write([]byte(body))
	})
}
