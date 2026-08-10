package sysmon

import "time"

// System is one whole-machine sample. Every field is a value type so a
// System can be copied freely between goroutines.
type System struct {
	CPU     CPU     `json:"cpu"`
	Memory  Memory  `json:"memory"`
	Load    Load    `json:"load"`
	Network Network `json:"network"`
	DiskIO  DiskIO  `json:"disk_io"`
	Disks   []Disk  `json:"disks"`
	// GPU is nil on any machine where no privileged agent is publishing
	// a reading, which is the default state. A pointer rather than a
	// zero-valued struct because "no GPU data" and "an idle GPU" are
	// different facts and a renderer must be able to tell them apart.
	GPU *GPU `json:"gpu,omitempty"`
}

// CPU is whole-machine processor utilisation over the sampling window.
// UsedPercent is user+sys and is scaled so 100 means "every core busy".
type CPU struct {
	Cores       int     `json:"cores"`
	UsedPercent float64 `json:"used_percent"`
	UserPercent float64 `json:"user_percent"`
	SysPercent  float64 `json:"sys_percent"`
	IdlePercent float64 `json:"idle_percent"`
}

// Memory is physical memory occupancy.
//
// UsedBytes follows each platform's own definition of "used" so the
// number agrees with the platform's own tools: macOS counts everything
// that is not free or speculative, which is why a healthy Mac sits near
// 95%. AvailableBytes is the honest headroom figure — memory a new
// allocation can take without swapping — and is what a renderer should
// show next to the percentage so the high number reads as normal.
type Memory struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsedPercent    float64 `json:"used_percent"`
	AvailableBytes uint64  `json:"available_bytes"`
	SwapTotalBytes uint64  `json:"swap_total_bytes"`
	SwapUsedBytes  uint64  `json:"swap_used_bytes"`
}

// Load is the 1/5/15-minute run-queue average.
type Load struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
}

// Network is aggregated throughput across every non-loopback interface.
// Interface names the busiest link so a single-line renderer has
// something concrete to show.
type Network struct {
	Interface        string  `json:"interface"`
	RxBytesPerSecond float64 `json:"rx_bytes_per_second"`
	TxBytesPerSecond float64 `json:"tx_bytes_per_second"`
	RxBytesTotal     uint64  `json:"rx_bytes_total"`
	TxBytesTotal     uint64  `json:"tx_bytes_total"`
}

// DiskIO is aggregated block-device throughput.
//
// ReadWriteSplit reports whether the platform exposes read and write
// separately: Linux does (/proc/diskstats), macOS iostat does not and
// only fills BytesPerSecond plus TransfersPerSecond. Renderers must
// check the flag rather than inferring it from two zero values, which
// is also what an idle disk looks like.
type DiskIO struct {
	BytesPerSecond      float64 `json:"bytes_per_second"`
	ReadBytesPerSecond  float64 `json:"read_bytes_per_second"`
	WriteBytesPerSecond float64 `json:"write_bytes_per_second"`
	TransfersPerSecond  float64 `json:"transfers_per_second"`
	ReadWriteSplit      bool    `json:"read_write_split"`
}

// Disk is one mounted filesystem backed by a real device.
type Disk struct {
	Device      string  `json:"device"`
	Mount       string  `json:"mount"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
}

// Proc is one row of the OS process table.
type Proc struct {
	PID         int     `json:"pid"`
	PPID        int     `json:"ppid"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes uint64  `json:"memory_bytes"`
	State       string  `json:"state"`
	Command     string  `json:"command"`
	// GPUPercent is filled only where a GPU agent publishes per-process
	// readings; it is zero everywhere else, including on hardware that
	// cannot attribute GPU time at all.
	GPUPercent float64 `json:"gpu_percent"`
}

// Port is one listening socket owned by a process.
type Port struct {
	PID      int    `json:"pid"`
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	State    string `json:"state"`
}

// Task is one pm2-managed application joined with its OS detail: the
// descendant processes it spawned and the ports that whole tree listens
// on. Tree* fields sum the task's own usage with every descendant, which
// is the number that matters for a shell script that forks a real worker.
type Task struct {
	ID              int       `json:"id"`
	Namespace       string    `json:"namespace"`
	Name            string    `json:"name"`
	Status          string    `json:"status"`
	PID             int       `json:"pid"`
	Restarts        int       `json:"restarts"`
	CPUPercent      float64   `json:"cpu_percent"`
	MemoryBytes     uint64    `json:"memory_bytes"`
	GPUPercent      float64   `json:"gpu_percent"`
	TreeCPUPercent  float64   `json:"tree_cpu_percent"`
	TreeMemoryBytes uint64    `json:"tree_memory_bytes"`
	TreeGPUPercent  float64   `json:"tree_gpu_percent"`
	Command         string    `json:"command"`
	Children        []Proc    `json:"children"`
	Ports           []Port    `json:"ports"`
	StartedAt       time.Time `json:"started_at,omitzero"`
}

// Host is the static machine identity plus how long it has been up.
type Host struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	Cores         int    `json:"cores"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// ProcCounts summarises the OS process table.
type ProcCounts struct {
	Total   int `json:"total"`
	Running int `json:"running"`
}

// Snapshot is one complete observation of the machine and every managed
// application on it — the unit the periodic emitter writes and the TUI
// renders.
type Snapshot struct {
	Time      time.Time  `json:"time"`
	Host      Host       `json:"host"`
	System    System     `json:"system"`
	Processes ProcCounts `json:"processes"`
	Tasks     []Task     `json:"tasks"`
	// Errors records collectors that failed for this sample. A partial
	// snapshot is more useful than none, so a failing source degrades to
	// zero values plus one entry here.
	Errors []string `json:"errors,omitempty"`
}
