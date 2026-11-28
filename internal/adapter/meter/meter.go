package meter

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/O-Marsters-1997/command-center/internal/adapter/command"
)

const (
	cmdTimeout = 10 * time.Second
	maxArgs    = 200
	kib        = 1024
)

type Process struct {
	Pgid, Pid int
	Name      string
	Args      string
	RSS       int64
}

type Reading struct {
	Total, Used int64
	Procs       []Process
}

type Meter interface {
	Read(ctx context.Context, pgids []int) (Reading, error)
}

type PS struct{}

var _ Meter = PS{}

func (PS) Read(ctx context.Context, pgids []int) (Reading, error) {
	total, used, err := machineMemory(ctx)
	if err != nil {
		return Reading{}, fmt.Errorf("read machine memory: %w", err)
	}
	r := Reading{Total: total, Used: used}
	if len(pgids) == 0 {
		return r, nil
	}
	out, err := command.OutputWithin(ctx, cmdTimeout, "", "ps", "-axo", "pgid=,pid=,rss=,args=")
	if err != nil {
		return Reading{}, fmt.Errorf("run ps: %w", err)
	}
	r.Procs, err = parsePS(out, pgids)
	if err != nil {
		return Reading{}, fmt.Errorf("parse ps output: %w", err)
	}
	return r, nil
}

func parsePS(out []byte, pgids []int) ([]Process, error) {
	var procs []Process
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		rest := strings.TrimSpace(sc.Text())
		var nums [3]int64
		for i := range nums {
			field, after, _ := strings.Cut(rest, " ")
			n, err := strconv.ParseInt(field, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("line %q: %w", sc.Text(), err)
			}
			nums[i], rest = n, strings.TrimLeft(after, " ")
		}
		pgid := int(nums[0])
		if !slices.Contains(pgids, pgid) || rest == "" {
			continue
		}
		exe, _, _ := strings.Cut(rest, " ")
		procs = append(procs, Process{
			Pgid: pgid, Pid: int(nums[1]), Name: filepath.Base(exe),
			Args: truncate(rest), RSS: nums[2] * kib,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan ps output: %w", err)
	}
	return procs, nil
}

func truncate(s string) string {
	if r := []rune(s); len(r) > maxArgs {
		return string(r[:maxArgs])
	}
	return s
}

func machineMemory(ctx context.Context) (total, used int64, err error) {
	switch runtime.GOOS {
	case "darwin":
		return darwinMemory(ctx)
	case "linux":
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0, 0, fmt.Errorf("read /proc/meminfo: %w", err)
		}
		return parseMeminfo(data)
	default:
		return 0, 0, fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
}

func darwinMemory(ctx context.Context) (total, used int64, err error) {
	out, err := command.OutputWithin(ctx, cmdTimeout, "", "sysctl", "-n", "hw.memsize")
	if err != nil {
		return 0, 0, fmt.Errorf("run sysctl: %w", err)
	}
	total, err = strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse hw.memsize: %w", err)
	}
	out, err = command.OutputWithin(ctx, cmdTimeout, "", "vm_stat")
	if err != nil {
		return 0, 0, fmt.Errorf("run vm_stat: %w", err)
	}
	used, err = parseVMStat(out)
	return total, used, err
}

func parseVMStat(out []byte) (int64, error) {
	var pageSize, pages int64
	var counters int
	for line := range strings.Lines(string(out)) {
		switch {
		case strings.HasPrefix(line, "Mach Virtual Memory Statistics"):
			_, after, _ := strings.Cut(line, "page size of ")
			size, _, _ := strings.Cut(after, " ")
			n, err := strconv.ParseInt(size, 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse vm_stat page size %q: %w", size, err)
			}
			pageSize = n
		case strings.HasPrefix(line, "Pages active:"),
			strings.HasPrefix(line, "Pages wired down:"),
			strings.HasPrefix(line, "Pages occupied by compressor:"):
			_, val, _ := strings.Cut(line, ":")
			n, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(val), "."), 10, 64)
			if err != nil {
				return 0, fmt.Errorf("parse vm_stat line %q: %w", line, err)
			}
			pages += n
			counters++
		}
	}
	if pageSize == 0 || counters != 3 {
		return 0, errors.New("vm_stat output missing page size or counters")
	}
	return pages * pageSize, nil
}

func parseMeminfo(data []byte) (total, used int64, err error) {
	avail := int64(-1)
	for line := range strings.Lines(string(data)) {
		key, val, ok := strings.Cut(line, ":")
		if !ok || (key != "MemTotal" && key != "MemAvailable") {
			continue
		}
		fields := strings.Fields(val)
		if len(fields) == 0 {
			return 0, 0, fmt.Errorf("meminfo %s has no value", key)
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse meminfo %s: %w", key, err)
		}
		if key == "MemTotal" {
			total = n * kib
		} else {
			avail = n * kib
		}
	}
	if total == 0 || avail < 0 {
		return 0, 0, errors.New("meminfo missing MemTotal or MemAvailable")
	}
	return total, total - avail, nil
}
