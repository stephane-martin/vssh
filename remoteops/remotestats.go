package remoteops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// credits to https://github.com/rapidloop/rtop

func runCommand(ctx context.Context, client *ssh.Client, command string) ([]string, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	var canceled int32
	go func() {
		<-ctx.Done()
		atomic.StoreInt32(&canceled, 1)
		_ = session.Close()
	}()
	var buf bytes.Buffer
	var buferr bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buferr
	err = session.Run(command)
	if atomic.LoadInt32(&canceled) == 1 {
		return nil, context.Canceled
	}
	_ = session.Close()
	if err != nil {
		stderr := strings.TrimSpace(buferr.String())
		if stderr != "" {
			return nil, errors.New(stderr)
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return lines, nil
}

type FSInfo struct {
	MountPoint string
	Used       uint64
	Free       uint64
}

func (fs FSInfo) Total() uint64 {
	return fs.Used + fs.Free
}

type NetInfo struct {
	Name string
	IPv4 []string
	IPv6 []string
	Rx   uint64
	Tx   uint64
}

type cpuRaw struct {
	User    uint64 // time spent in user mode
	System  uint64 // time spent in system mode
	Idle    uint64 // time spent in the idle task
	Iowait  uint64 // time spent waiting for I/O to complete (since Linux 2.5.41)
	Irq     uint64 // time spent servicing  interrupts  (since  2.6.0-test4)
	SoftIrq uint64 // time spent servicing softirqs (since 2.6.0-test4)
	Steal   uint64 // time spent in other OSes when running in a virtualized environment
	Guest   uint64 // time spent running a virtual CPU for guest operating systems under the control of the Linux kernel.
	Total   uint64 // total of all time fields
}

type CPUInfo struct {
	User    float32
	Nice    float32
	System  float32
	Idle    float32
	Iowait  float32
	Irq     float32
	SoftIrq float32
	Steal   float32
	Guest   float32
}

type LinuxStater struct {
	client *ssh.Client
	preCPU cpuRaw
}

type OpenBSDStater struct {
	client *ssh.Client
}

type LoadInfo struct {
	Load1        string
	Load5        string
	Load10       string
	RunningProcs string
	TotalProcs   string
}

type MemInfo struct {
	MemTotal  uint64
	MemActive uint64
	SwapTotal uint64
	SwapFree  uint64
}

type Stats struct {
	Uptime   time.Duration
	Hostname string
	Load     LoadInfo
	Mem      MemInfo
	FS       []FSInfo
	Net      []NetInfo
	CPU      CPUInfo // or []CPUInfo to get all the cpu-core's stats?
}

type Stater interface {
	Get(context.Context) (Stats, error)
}

func NewStater(client *ssh.Client) (Stater, error) {
	lines, err := runCommand(context.Background(), client, "uname -s")
	if err != nil {
		return nil, err
	}
	line := strings.ToLower(lines[0])
	if line == "linux" {
		return &LinuxStater{client: client}, nil
	}
	if line == "openbsd" {
		return &OpenBSDStater{client: client}, nil
	}
	return nil, fmt.Errorf("unsupported OS: %s", line)
}

type Merror struct {
	Uptime   error
	Hostname error
	Load     error
	Mem      error
	FS       error
	Net      error
	CPU      error
}

func mErrorStr(errors ...error) string {
	var buf strings.Builder
	for _, e := range errors {
		if e != nil {
			buf.WriteString(e.Error())
			buf.WriteString("\n")
		}
	}
	return buf.String()
}

func (e *Merror) Error() string {
	if e == nil {
		return ""
	}
	return mErrorStr(e.Uptime, e.Hostname, e.Load, e.Mem, e.FS, e.Net, e.CPU)
}

func (e *Merror) IsZero() bool {
	if e == nil {
		return true
	}
	if e.Uptime == nil && e.Hostname == nil && e.Load == nil && e.Mem == nil && e.FS == nil && e.Net == nil && e.CPU == nil {
		return true
	}
	return false
}

func (e *Merror) IsCanceled() bool {
	return e.Uptime == context.Canceled || e.Hostname == context.Canceled ||
		e.Load == context.Canceled || e.Mem == context.Canceled ||
		e.FS == context.Canceled || e.Net == context.Canceled || e.CPU == context.Canceled
}

func errorf(s string, err error) error {
	if err == context.Canceled {
		return context.Canceled
	}
	return fmt.Errorf(s, err)
}

func (s *LinuxStater) Get(ctx context.Context) (Stats, error) {
	var stats Stats
	var merr Merror
	stats.Uptime, merr.Uptime = s.getUptime(ctx)
	stats.Hostname, merr.Hostname = s.getHostname(ctx)
	stats.Load, merr.Load = s.getLoadInfo(ctx)
	stats.Mem, merr.Mem = s.getMemInfo(ctx)
	stats.FS, merr.FS = s.getFSInfos(ctx)
	stats.Net, merr.Net = s.getNetInfos(ctx)
	stats.CPU, merr.CPU = s.getCPUInfo(ctx)
	if merr.IsZero() {
		return stats, nil
	}
	if merr.IsCanceled() {
		return stats, context.Canceled
	}
	return stats, &merr
}

func (s *OpenBSDStater) Get(ctx context.Context) (Stats, error) {
	var stats Stats
	var merr Merror
	stats.Uptime, merr.Uptime = s.getUptime(ctx)
	stats.Hostname, merr.Hostname = s.getHostname(ctx)
	stats.Load, merr.Load = s.getLoadInfo(ctx)
	stats.Mem, merr.Mem = s.getMemInfo(ctx)
	stats.FS, merr.FS = s.getFSInfos(ctx)
	stats.Net, merr.Net = s.getNetInfos(ctx)
	stats.CPU, merr.CPU = s.getCPUInfo(ctx)
	if merr.IsZero() {
		return stats, nil
	}
	if merr.IsCanceled() {
		return stats, context.Canceled
	}
	return stats, &merr
}

var z time.Duration

func (s *LinuxStater) getUptime(ctx context.Context) (time.Duration, error) {
	uptime, err := runCommand(ctx, s.client, "cat /proc/uptime")
	if err != nil {
		return z, errorf("cat /proc/uptime: %s", err)
	}

	parts := strings.Fields(uptime[0])
	if len(parts) == 0 {
		return z, errors.New("inconsistent /proc/uptime")
	}
	upsecs, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return z, err
	}
	return time.Duration(upsecs * float64(time.Second)), nil
}

func (s *OpenBSDStater) getUptime(ctx context.Context) (time.Duration, error) {
	currentDateStr, err := runCommand(ctx, s.client, "date +%s")
	if err != nil {
		return z, errorf("date: %s", err)
	}
	currentDate, err := strconv.ParseInt(currentDateStr[0], 10, 32)
	if err != nil {
		return z, errorf("failed convert seconds to integer: %s", err)
	}
	bootTimeStr, err := runCommand(ctx, s.client, "sysctl -n kern.boottime")
	if err != nil {
		return z, errorf("sysctl: %s", err)
	}
	bootTime, err := strconv.ParseInt(bootTimeStr[0], 10, 32)
	if err != nil {
		return z, errorf("failed convert seconds to integer: %s", err)
	}
	return time.Duration(currentDate-bootTime) * time.Second, nil
}

func (s *LinuxStater) getHostname(ctx context.Context) (string, error) {
	hostname, err := runCommand(ctx, s.client, "hostname")
	if err == nil {
		return strings.TrimSpace(hostname[0]), nil
	}
	return "", errorf("hostname: %s", err)
}

func (s *OpenBSDStater) getHostname(ctx context.Context) (string, error) {
	hostname, err := runCommand(ctx, s.client, "hostname")
	if err == nil {
		return strings.TrimSpace(hostname[0]), nil
	}
	return "", errorf("hostname: %s", err)
}

func (s *LinuxStater) getLoadInfo(ctx context.Context) (LoadInfo, error) {
	var l LoadInfo
	lines, err := runCommand(ctx, s.client, "cat /proc/loadavg")
	if err != nil {
		return l, errorf("cat /proc/loadavg: %s", err)
	}
	parts := strings.Fields(lines[0])
	if len(parts) < 5 {
		return l, errors.New("inconsistent /proc/loadavg")
	}
	l.Load1 = parts[0]
	l.Load5 = parts[1]
	l.Load10 = parts[2]
	if i := strings.Index(parts[3], "/"); i != -1 {
		l.RunningProcs = parts[3][0:i]
		if i+1 < len(parts[3]) {
			l.TotalProcs = parts[3][i+1:]
		}
	}
	return l, nil
}

func (s *OpenBSDStater) getLoadInfo(ctx context.Context) (LoadInfo, error) {
	var l LoadInfo
	uptimeLines, err := runCommand(ctx, s.client, "uptime")
	if err != nil {
		return l, errorf("uptime: %s", err)
	}
	vmStatLines, err := runCommand(ctx, s.client, "vmstat")
	if err != nil {
		return l, errorf("vmstat: %s", err)
	}

	spl := strings.Split(uptimeLines[0], ":")
	last := strings.TrimSpace(spl[len(spl)-1])
	spl = strings.Split(last, ",")
	if len(spl) < 3 {
		return l, errors.New("inconsistent uptime line")
	}
	l.Load1 = strings.TrimSpace(spl[0])
	l.Load5 = strings.TrimSpace(spl[1])
	l.Load10 = strings.TrimSpace(spl[2])

	for _, line := range vmStatLines {
		fields := strings.Fields(line)
		if len(fields) > 1 {
			running, e1 := strconv.ParseInt(fields[0], 10, 32)
			sleeping, e2 := strconv.ParseInt(fields[1], 10, 32)
			if e1 == nil && e2 == nil {
				l.RunningProcs = fmt.Sprintf("%d", running)
				l.TotalProcs = fmt.Sprintf("%d", running+sleeping)
				break
			}
		}
	}
	return l, nil
}

func (s *OpenBSDStater) getMemInfo(ctx context.Context) (MemInfo, error) {
	var m MemInfo
	pm, err := runCommand(ctx, s.client, "sysctl -n hw.physmem")
	if err != nil {
		return m, errorf("sysctl: %s", err)
	}
	m.MemTotal, err = strconv.ParseUint(pm[0], 10, 64)
	if err != nil {
		return m, errors.New("inconsistent sysctl hw.physmem")
	}
	sw, err := runCommand(ctx, s.client, "swapctl -s -k")
	if err != nil {
		return m, errorf("swapctl: %s", err)
	}
	fields := strings.Fields(sw[0])
	if len(fields) < 7 {
		return m, errors.New("inconsistent swapctl: not enough fields")
	}
	m.SwapTotal, err = strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return m, errors.New("inconsistent swapctl: swap total")
	}
	m.SwapTotal = m.SwapTotal * 1024
	m.SwapFree, err = strconv.ParseUint(fields[6], 10, 64)
	if err != nil {
		return m, errors.New("inconsistent swapctl: swap free")
	}
	m.SwapFree = m.SwapFree * 1024

	vmLines, err := runCommand(ctx, s.client, "vmstat -s")
	if err != nil {
		return m, errorf("vmstat: %s", err)
	}
	var active uint64
	var bytesPerPage uint64
	for _, line := range vmLines {
		if strings.HasSuffix(line, "pages active") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				active, _ = strconv.ParseUint(fields[0], 10, 64)
			}
		}
		if strings.HasSuffix(line, "bytes per page") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				bytesPerPage, _ = strconv.ParseUint(fields[0], 10, 64)
			}
		}
	}
	m.MemActive = active * bytesPerPage
	return m, nil
}

func (s *LinuxStater) getMemInfo(ctx context.Context) (MemInfo, error) {
	var m MemInfo
	lines, err := runCommand(ctx, s.client, "cat /proc/meminfo")
	if err != nil {
		return m, errorf("cat /proc/meminfo: %s", err)
	}

	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) == 3 {
			val, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				continue
			}
			val *= 1024
			switch parts[0] {
			case "MemTotal:":
				m.MemTotal = val
			case "Active:":
				m.MemActive = val
			case "SwapTotal:":
				m.SwapTotal = val
			case "SwapFree:":
				m.SwapFree = val
			}
		}
	}

	return m, nil
}

func (s *LinuxStater) getFSInfos(ctx context.Context) ([]FSInfo, error) {
	return getFSInfos(ctx, s.client)
}

func (s *OpenBSDStater) getFSInfos(ctx context.Context) ([]FSInfo, error) {
	return getFSInfos(ctx, s.client)
}

func getFSInfos(ctx context.Context, client *ssh.Client) ([]FSInfo, error) {
	lines, err := runCommand(ctx, client, "BLOCKSIZE=1024 df")
	if err != nil {
		return nil, errorf("df: %s", err)
	}
	var fsinfos []FSInfo

	flag := 0
	for _, line := range lines {
		parts := strings.Fields(line)
		n := len(parts)
		dev := n > 0 && strings.Index(parts[0], "/dev/") == 0
		if n == 1 && dev {
			flag = 1
		} else if (n == 5 && flag == 1) || (n == 6 && dev) {
			i := flag
			flag = 0
			used, err := strconv.ParseUint(parts[2-i], 10, 64)
			if err != nil {
				continue
			}
			free, err := strconv.ParseUint(parts[3-i], 10, 64)
			if err != nil {
				continue
			}
			fsinfos = append(fsinfos, FSInfo{
				parts[5-i], used * 1024, free * 1024,
			})
		}
	}
	sort.Slice(fsinfos, func(i, j int) bool {
		return fsinfos[i].MountPoint < fsinfos[j].MountPoint
	})

	return fsinfos, nil
}

func (s *OpenBSDStater) getNetInfos(ctx context.Context) ([]NetInfo, error) {
	lines, err := runCommand(ctx, s.client, "netstat -bin")
	if err != nil {
		return nil, errorf("netstat: %s", err)
	}
	if len(lines) == 0 {
		return nil, errors.New("inconsistent netstat")
	}
	infos := make(map[string]*NetInfo)
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		name := fields[0]
		if _, ok := infos[name]; !ok {
			infos[name] = &NetInfo{Name: name}
		}
		ip := net.ParseIP(fields[3])
		if ip != nil {
			ipv4 := ip.To4()
			if ipv4 != nil {
				infos[name].IPv4 = append(infos[name].IPv4, ipv4.String())
			} else {
				infos[name].IPv6 = append(infos[name].IPv6, ip.String())
			}
			rx, err := strconv.ParseUint(fields[4], 10, 64)
			if err == nil {
				infos[name].Rx = rx
			}
			tx, err := strconv.ParseUint(fields[5], 10, 64)
			if err == nil {
				infos[name].Tx = tx
			}
		}
	}
	result := make([]NetInfo, 0, len(infos))
	for _, v := range infos {
		result = append(result, *v)
	}
	return result, nil
}

func (s *LinuxStater) getNetInfos(ctx context.Context) ([]NetInfo, error) {
	lines, err := runCommand(ctx, s.client, "ip -o addr")
	if err != nil {
		// try /sbin/ip
		lines, err = runCommand(ctx, s.client, "/sbin/ip -o addr")
		if err != nil {
			return nil, errorf("ip -o addr: %s", err)
		}
	}

	netinfos := make(map[string]*NetInfo)

	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) >= 4 && (parts[2] == "inet" || parts[2] == "inet6") {
			ipv4 := parts[2] == "inet"
			name := parts[1]
			if _, ok := netinfos[name]; !ok {
				netinfos[name] = &NetInfo{Name: name}
			}
			if ipv4 {
				netinfos[name].IPv4 = append(netinfos[name].IPv4, parts[3])
			} else {
				netinfos[name].IPv6 = append(netinfos[name].IPv6, parts[3])
			}
		}
	}

	lines, err = runCommand(ctx, s.client, "cat /proc/net/dev")
	if err != nil {
		return nil, errorf("cat /proc/net/dev: %s", err)
	}

	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) == 17 {
			name := strings.TrimSpace(parts[0])
			name = strings.TrimSuffix(name, ":")
			if _, ok := netinfos[name]; ok {
				rx, err := strconv.ParseUint(parts[1], 10, 64)
				if err != nil {
					continue
				}
				tx, err := strconv.ParseUint(parts[9], 10, 64)
				if err != nil {
					continue
				}
				netinfos[name].Rx = rx
				netinfos[name].Tx = tx
			}
		}
	}
	result := make([]NetInfo, 0, len(netinfos))
	for _, v := range netinfos {
		result = append(result, *v)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func parseCPUFields(fields []string) cpuRaw {
	var s cpuRaw
	numFields := len(fields)
	for i := 1; i < numFields; i++ {
		val, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			continue
		}

		s.Total += val
		switch i {
		case 1:
			s.User = val
		case 3:
			s.System = val
		case 4:
			s.Idle = val
		case 5:
			s.Iowait = val
		case 6:
			s.Irq = val
		case 7:
			s.SoftIrq = val
		case 8:
			s.Steal = val
		case 9:
			s.Guest = val
		}
	}
	return s
}

func (s *OpenBSDStater) getCPUInfo(ctx context.Context) (CPUInfo, error) {
	var cpu CPUInfo
	lines, err := runCommand(ctx, s.client, "vmstat")
	if err != nil {
		return cpu, errorf("vmstat: %s", err)
	}
	if len(lines) < 3 {
		return cpu, errors.New("inconsistent vmstat: number of lines")
	}
	fields := strings.Fields(lines[2])
	if len(fields) < 18 {
		return cpu, errors.New("inconsistent vmstat: number of fields")
	}
	idle, err := strconv.ParseUint(fields[17], 10, 32)
	if err == nil {
		cpu.Idle = float32(idle)
	}
	sys, err := strconv.ParseUint(fields[16], 10, 32)
	if err == nil {
		cpu.System = float32(sys)
	}
	user, err := strconv.ParseUint(fields[15], 10, 32)
	if err == nil {
		cpu.User = float32(user)
	}
	return cpu, nil
}

func (s *LinuxStater) getCPUInfo(ctx context.Context) (CPUInfo, error) {
	var cpu CPUInfo
	lines, err := runCommand(ctx, s.client, "cat /proc/stat")
	if err != nil {
		return cpu, errorf("cat /proc/stat: %s", err)
	}

	var nowCPU cpuRaw

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "cpu" { // changing here if want to get every cpu-core's stats
			nowCPU = parseCPUFields(fields)
			break
		}
	}
	if s.preCPU.Total == 0 {
		s.preCPU = nowCPU
		return cpu, nil
	}

	t := nowCPU.Total - s.preCPU.Total
	if t == 0 {
		s.preCPU = nowCPU
		return cpu, nil
	}
	total := float32(t)
	cpu.User = float32(nowCPU.User-s.preCPU.User) / total * 100
	cpu.System = float32(nowCPU.System-s.preCPU.System) / total * 100
	cpu.Idle = float32(nowCPU.Idle-s.preCPU.Idle) / total * 100
	cpu.Iowait = float32(nowCPU.Iowait-s.preCPU.Iowait) / total * 100
	cpu.Irq = float32(nowCPU.Irq-s.preCPU.Irq) / total * 100
	cpu.SoftIrq = float32(nowCPU.SoftIrq-s.preCPU.SoftIrq) / total * 100
	cpu.Guest = float32(nowCPU.Guest-s.preCPU.Guest) / total * 100

	s.preCPU = nowCPU
	return cpu, nil
}
