package api

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/pcim/pc-inventory-manager/internal/server/store"
)

// report is any renderable report.
type report interface {
	Title() string
	Columns() []string
	Rows() [][]string
	Sections() []reportSection
	WriteCSV(w io.Writer) error
	PDF() ([]byte, error)
	HTML() string
}

type reportSection struct {
	Heading string
	Columns []string
	Rows    [][]string
}

func (s *Server) WriteReportCSV(w io.Writer, rep report) error { return rep.WriteCSV(w) }

func writeCSV(w io.Writer, cols []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(cols); err != nil {
		return err
	}
	for _, r := range rows {
		if err := cw.Write(r); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func (s *reportSection) csv(w io.Writer) error {
	if s.Heading != "" {
		fmt.Fprintf(w, "\n# %s\n", s.Heading)
	}
	return writeCSV(w, s.Columns, s.Rows)
}

// --- Hardware inventory report ------------------------------------------

type inventoryReport struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	ServerName  string          `json:"serverName"`
	Computers   []inventoryItem `json:"computers"`
}

type inventoryItem struct {
	AgentID         string `json:"agentId"`
	Hostname        string `json:"hostname"`
	DisplayName     string `json:"displayName"`
	Platform        string `json:"platform"`
	OS              string `json:"os"`
	OSEdition       string `json:"osEdition"`
	OSVersion       string `json:"osVersion"`
	OSBuild         string `json:"osBuild"`
	Arch            string `json:"arch"`
	CPUManufacturer string `json:"cpuManufacturer"`
	CPUBrand        string `json:"cpuBrand"`
	CPUCores        int    `json:"cpuPhysicalCores"`
	CPUThreads      int    `json:"cpuLogicalCores"`
	RAMBytes        uint64 `json:"ramBytes"`
	RAMType         string `json:"ramType"`
	RAMModules      int    `json:"ramModules"`
	GPUs            string `json:"gpus"`
	VRAMBytes       uint64 `json:"vramBytes"`
	Motherboard     string `json:"motherboard"`
	SystemModel     string `json:"systemModel"`
	Disks           string `json:"disks"`
	DiskCount       int    `json:"diskCount"`
	DiskMedia       string `json:"diskMedia"`
	StorageBytes    uint64 `json:"storageTotalBytes"`
	FreeBytes       uint64 `json:"storageFreeBytes"`
	Volumes         string `json:"volumes"`
	PrimaryIP       string `json:"primaryIp"`
	MACs            string `json:"macs"`
	Monitors        int    `json:"monitorCount"`
	Software        int    `json:"softwareCount"`
	Location        string `json:"location"`
	Group           string `json:"group"`
	Status          string `json:"status"`
	AgentVersion    string `json:"agentVersion"`
	EnrolledAt      string `json:"enrolledAt"`
}

func buildInventoryReport(ctx context.Context, s *Server, rows []*store.InventoryRow) inventoryReport {
	rep := inventoryReport{GeneratedAt: time.Now().UTC(), ServerName: s.serverName(ctx)}
	for _, r := range rows {
		item := inventoryItem{
			AgentID: r.AgentID, Hostname: r.Hostname, DisplayName: r.DisplayName, Platform: r.Platform, OS: r.OSName, OSEdition: r.OSEdition,
			OSVersion: r.OSVersion, Arch: r.Arch, CPUManufacturer: r.CPUManufacturer, CPUBrand: r.CPUBrand, CPUCores: r.CPUPhysicalCores,
			CPUThreads: r.CPULogicalCores, RAMBytes: r.RAMBytes, RAMType: r.RAMType, GPUs: r.GPUNames, Motherboard: r.Motherboard,
			SystemModel: r.SystemModel, DiskCount: r.DiskCount, DiskMedia: r.DiskMedia, StorageBytes: r.StorageTotal, FreeBytes: r.StorageFree,
			PrimaryIP: r.PrimaryIP, MACs: r.PrimaryMAC, Monitors: r.MonitorCount, Software: r.SoftwareCount, Location: r.Location,
			Group: r.GroupName, Status: r.Status, AgentVersion: r.AgentVersion, EnrolledAt: r.EnrolledAt.Local().Format("2006-01-02 15:04"),
		}
		if snap, err := s.Store.LatestSnapshot(ctx, r.AgentID); err == nil {
			inv := snap.Inventory
			item.OSBuild = inv.OS.Build
			item.RAMModules = len(inv.Memory.Modules)
			if inv.GPUs != nil {
				var names []string
				for _, g := range inv.GPUs {
					names = append(names, g.Name)
					if g.VRAMBytes != nil && *g.VRAMBytes > item.VRAMBytes {
						item.VRAMBytes = *g.VRAMBytes
					}
				}
				item.GPUs = strings.Join(names, "; ")
			}
			var disks []string
			for _, d := range inv.PhysicalDisks {
				disks = append(disks, fmt.Sprintf("%s %s %s %s", d.Manufacturer, d.Model, humanBytes(d.CapacityBytes), d.MediaType))
			}
			item.Disks = strings.Join(disks, "; ")
			var vols []string
			for _, v := range inv.Volumes {
				vols = append(vols, fmt.Sprintf("%s %.0f%%", v.MountPoint, v.UsagePercent))
			}
			item.Volumes = strings.Join(vols, "; ")
		}
		rep.Computers = append(rep.Computers, item)
	}
	return rep
}

func (r inventoryReport) Title() string { return "Hardware Inventory Report" }

func (r inventoryReport) Columns() []string {
	return []string{"Computer", "Platform", "OS", "Edition", "Version", "Build", "Arch", "CPU", "CPU Manufacturer", "Cores", "Threads",
		"RAM", "RAM Type", "RAM Modules", "GPU", "VRAM", "Motherboard", "System Model", "Disks", "Disk Media", "Storage Total", "Storage Free",
		"Volumes", "Primary IP", "MAC", "Monitors", "Software", "Location", "Group", "Status", "Agent Version"}
}

func (r inventoryReport) Rows() [][]string {
	var out [][]string
	for _, c := range r.Computers {
		name := c.DisplayName
		if name == "" {
			name = c.Hostname
		}
		out = append(out, []string{
			name, c.Platform, c.OS, c.OSEdition, c.OSVersion, c.OSBuild, c.Arch, c.CPUBrand, c.CPUManufacturer,
			fmt.Sprint(c.CPUCores), fmt.Sprint(c.CPUThreads), humanBytes(c.RAMBytes), c.RAMType, fmt.Sprint(c.RAMModules),
			c.GPUs, humanBytes(c.VRAMBytes), c.Motherboard, c.SystemModel, c.Disks, c.DiskMedia,
			humanBytes(c.StorageBytes), humanBytes(c.FreeBytes), c.Volumes, c.PrimaryIP, c.MACs,
			fmt.Sprint(c.Monitors), fmt.Sprint(c.Software), c.Location, c.Group, c.Status, c.AgentVersion,
		})
	}
	return out
}

func (r inventoryReport) Sections() []reportSection { return nil }

func (r inventoryReport) WriteCSV(w io.Writer) error { return writeCSV(w, r.Columns(), r.Rows()) }

func (r inventoryReport) PDF() ([]byte, error) {
	return renderPDF(r.Title(), r.Sections(), r.Columns(), r.Rows(), len(r.Computers))
}
func (r inventoryReport) HTML() string {
	return renderHTML(r.Title(), r.Sections(), r.Columns(), r.Rows())
}

// --- Network summary ------------------------------------------------------

type networkReport struct {
	GeneratedAt time.Time     `json:"generatedAt"`
	Computers   []networkItem `json:"computers"`
}

type networkItem struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	IPs      string `json:"ips"`
	MACs     string `json:"macs"`
	Status   string `json:"status"`
	Online   bool   `json:"online"`
	LastSeen string `json:"lastSeen"`
	Location string `json:"location"`
	Group    string `json:"group"`
	Platform string `json:"platform"`
}

func buildNetworkReport(rows []*store.InventoryRow) networkReport {
	rep := networkReport{GeneratedAt: time.Now().UTC()}
	for _, r := range rows {
		last := ""
		if r.LastHeartbeatAt != nil {
			last = r.LastHeartbeatAt.Local().Format("2006-01-02 15:04")
		}
		name := r.DisplayName
		if name == "" {
			name = r.Hostname
		}
		rep.Computers = append(rep.Computers, networkItem{Hostname: name, OS: strings.TrimSpace(r.OSName + " " + r.OSVersion), IPs: r.IPs, MACs: r.PrimaryMAC, Status: r.Status, Online: r.Online, LastSeen: last, Location: r.Location, Group: r.GroupName, Platform: r.Platform})
	}
	return rep
}

func (r networkReport) Title() string { return "Network Summary" }
func (r networkReport) Columns() []string {
	return []string{"Computer", "Platform", "Operating System", "IP Addresses", "MAC", "Status", "Online", "Last Seen", "Location", "Group"}
}
func (r networkReport) Rows() [][]string {
	var out [][]string
	for _, c := range r.Computers {
		online := "No"
		if c.Online {
			online = "Yes"
		}
		out = append(out, []string{c.Hostname, c.Platform, c.OS, c.IPs, c.MACs, c.Status, online, c.LastSeen, c.Location, c.Group})
	}
	return out
}
func (r networkReport) Sections() []reportSection  { return nil }
func (r networkReport) WriteCSV(w io.Writer) error { return writeCSV(w, r.Columns(), r.Rows()) }
func (r networkReport) PDF() ([]byte, error) {
	return renderPDF(r.Title(), nil, r.Columns(), r.Rows(), len(r.Computers))
}
func (r networkReport) HTML() string { return renderHTML(r.Title(), nil, r.Columns(), r.Rows()) }

// --- Storage report -------------------------------------------------------

type storageReport struct {
	GeneratedAt   time.Time     `json:"generatedAt"`
	Computers     []storageItem `json:"computers"`
	TotalBytes    uint64        `json:"totalBytes"`
	FreeBytes     uint64        `json:"freeBytes"`
	LowSpaceCount int           `json:"lowSpaceCount"`
}

type storageItem struct {
	Hostname   string  `json:"hostname"`
	Disks      string  `json:"disks"`
	TotalBytes uint64  `json:"totalBytes"`
	FreeBytes  uint64  `json:"freeBytes"`
	UsedPct    float64 `json:"usedPercent"`
	Low        bool    `json:"low"`
	Volumes    string  `json:"volumes"`
}

func buildStorageReport(rows []*store.InventoryRow) storageReport {
	rep := storageReport{GeneratedAt: time.Now().UTC()}
	for _, r := range rows {
		item := storageItem{Hostname: r.Hostname, TotalBytes: r.StorageTotal, FreeBytes: r.StorageFree}
		if r.StorageTotal > 0 {
			item.UsedPct = float64(r.StorageTotal-r.StorageFree) / float64(r.StorageTotal) * 100
			item.Low = float64(r.StorageFree)/float64(r.StorageTotal) < 0.10
		}
		if item.Low {
			rep.LowSpaceCount++
		}
		rep.TotalBytes += r.StorageTotal
		rep.FreeBytes += r.StorageFree
		rep.Computers = append(rep.Computers, item)
	}
	return rep
}

func (r storageReport) Title() string { return "Storage Report" }
func (r storageReport) Columns() []string {
	return []string{"Computer", "Total Storage", "Free Storage", "Used %", "Low Space"}
}
func (r storageReport) Rows() [][]string {
	var out [][]string
	for _, c := range r.Computers {
		low := "No"
		if c.Low {
			low = "YES"
		}
		out = append(out, []string{c.Hostname, humanBytes(c.TotalBytes), humanBytes(c.FreeBytes), fmt.Sprintf("%.1f", c.UsedPct), low})
	}
	return out
}
func (r storageReport) Sections() []reportSection {
	return []reportSection{{Heading: fmt.Sprintf("Fleet total: %s installed, %s free, %d computers below 10%% free", humanBytes(r.TotalBytes), humanBytes(r.FreeBytes), r.LowSpaceCount)}}
}
func (r storageReport) WriteCSV(w io.Writer) error {
	for _, s := range r.Sections() {
		fmt.Fprintf(w, "# %s\n", s.Heading)
	}
	return writeCSV(w, r.Columns(), r.Rows())
}
func (r storageReport) PDF() ([]byte, error) {
	return renderPDF(r.Title(), r.Sections(), r.Columns(), r.Rows(), len(r.Computers))
}
func (r storageReport) HTML() string {
	return renderHTML(r.Title(), r.Sections(), r.Columns(), r.Rows())
}

// --- Distribution report --------------------------------------------------

type distributionReport struct {
	GeneratedAt time.Time           `json:"generatedAt"`
	CPU         []distributionEntry `json:"cpu"`
	GPU         []distributionEntry `json:"gpu"`
	RAM         []distributionEntry `json:"ram"`
	Storage     []distributionEntry `json:"storage"`
	OS          []distributionEntry `json:"os"`
	Media       []distributionEntry `json:"media"`
}

type distributionEntry struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func buildDistributionReport(rows []*store.InventoryRow) distributionReport {
	cpu := map[string]int{}
	gpu := map[string]int{}
	ram := map[string]int{}
	storage := map[string]int{}
	osd := map[string]int{}
	media := map[string]int{}
	for _, r := range rows {
		cpu[orUnknown(r.CPUBrand)]++
		for _, g := range splitSemi(r.GPUNames) {
			gpu[orUnknown(g)]++
		}
		ram[bucketRAM(r.RAMBytes)]++
		storage[bucketStorage(r.StorageTotal)]++
		osd[orUnknown(r.OSName)]++
		for _, m := range splitSemi(r.DiskMedia) {
			media[m]++
		}
	}
	conv := func(m map[string]int) []distributionEntry {
		var out []distributionEntry
		for k, v := range m {
			out = append(out, distributionEntry{Name: k, Count: v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
		return out
	}
	return distributionReport{GeneratedAt: time.Now().UTC(), CPU: conv(cpu), GPU: conv(gpu), RAM: conv(ram), Storage: conv(storage), OS: conv(osd), Media: conv(media)}
}

func (r distributionReport) Title() string     { return "Hardware Distribution Report" }
func (r distributionReport) Columns() []string { return []string{"Category", "Value", "Computers"} }
func (r distributionReport) Rows() [][]string {
	var out [][]string
	appendAll := func(cat string, es []distributionEntry) {
		for _, e := range es {
			out = append(out, []string{cat, e.Name, fmt.Sprint(e.Count)})
		}
	}
	appendAll("Operating System", r.OS)
	appendAll("CPU", r.CPU)
	appendAll("GPU", r.GPU)
	appendAll("RAM", r.RAM)
	appendAll("Storage", r.Storage)
	appendAll("Disk Media", r.Media)
	return out
}
func (r distributionReport) Sections() []reportSection  { return nil }
func (r distributionReport) WriteCSV(w io.Writer) error { return writeCSV(w, r.Columns(), r.Rows()) }
func (r distributionReport) PDF() ([]byte, error) {
	return renderPDF(r.Title(), nil, r.Columns(), r.Rows(), len(r.Rows()))
}
func (r distributionReport) HTML() string { return renderHTML(r.Title(), nil, r.Columns(), r.Rows()) }

// --- Changes report -------------------------------------------------------

type changesReport struct {
	GeneratedAt time.Time              `json:"generatedAt"`
	Changes     []store.HardwareChange `json:"changes"`
}

func (r changesReport) Title() string { return "Hardware Change Report" }
func (r changesReport) Columns() []string {
	return []string{"Detected", "Computer", "Category", "Severity", "Change"}
}
func (r changesReport) Rows() [][]string {
	var out [][]string
	for _, c := range r.Changes {
		out = append(out, []string{c.DetectedAt.Local().Format("2006-01-02 15:04"), c.Hostname, c.Category, c.Severity, c.Summary})
	}
	return out
}
func (r changesReport) Sections() []reportSection  { return nil }
func (r changesReport) WriteCSV(w io.Writer) error { return writeCSV(w, r.Columns(), r.Rows()) }
func (r changesReport) PDF() ([]byte, error) {
	return renderPDF(r.Title(), nil, r.Columns(), r.Rows(), len(r.Changes))
}
func (r changesReport) HTML() string { return renderHTML(r.Title(), nil, r.Columns(), r.Rows()) }

// --- Performance report ---------------------------------------------------

type performanceReport struct {
	GeneratedAt time.Time            `json:"generatedAt"`
	Range       string               `json:"range"`
	From        time.Time            `json:"from"`
	To          time.Time            `json:"to"`
	Computers   []performanceSummary `json:"computers"`
}

type performanceSummary struct {
	Hostname     string  `json:"hostname"`
	Samples      int     `json:"samples"`
	CPUAvg       float64 `json:"cpuAvg"`
	CPUMax       float64 `json:"cpuMax"`
	RAMAvg       float64 `json:"ramAvg"`
	RAMMax       float64 `json:"ramMax"`
	DiskReadAvg  float64 `json:"diskReadAvgBps"`
	DiskWriteAvg float64 `json:"diskWriteAvgBps"`
	NetRxAvg     float64 `json:"netRxAvgBps"`
	NetTxAvg     float64 `json:"netTxAvgBps"`
	LastCPU      float64 `json:"lastCpu"`
	LastRAM      float64 `json:"lastRam"`
}

func buildPerformanceReport(ctx context.Context, s *Server, rows []*store.InventoryRow, from, to time.Time, rangeKey string) performanceReport {
	rep := performanceReport{GeneratedAt: time.Now().UTC(), Range: rangeKey, From: from, To: to}
	for _, r := range rows {
		series, _, err := s.Store.PerfSeries(ctx, r.AgentID, from, to)
		if err != nil || len(series) == 0 {
			continue
		}
		sum := performanceSummary{Hostname: r.Hostname, Samples: len(series)}
		for _, p := range series {
			sum.CPUAvg += p.CPUPercent
			sum.RAMAvg += p.MemPercent
			sum.DiskReadAvg += p.DiskReadBps
			sum.DiskWriteAvg += p.DiskWriteBps
			sum.NetRxAvg += p.NetRxBps
			sum.NetTxAvg += p.NetTxBps
			if p.CPUPercent > sum.CPUMax {
				sum.CPUMax = p.CPUPercent
			}
			if p.MemPercent > sum.RAMMax {
				sum.RAMMax = p.MemPercent
			}
			sum.LastCPU = p.CPUPercent
			sum.LastRAM = p.MemPercent
		}
		n := float64(len(series))
		sum.CPUAvg /= n
		sum.RAMAvg /= n
		sum.DiskReadAvg /= n
		sum.DiskWriteAvg /= n
		sum.NetRxAvg /= n
		sum.NetTxAvg /= n
		rep.Computers = append(rep.Computers, sum)
	}
	return rep
}

func (r performanceReport) Title() string {
	return "Performance Report (" + r.Range + ")"
}
func (r performanceReport) Columns() []string {
	return []string{"Computer", "Samples", "CPU avg %", "CPU max %", "RAM avg %", "RAM max %", "Disk read avg", "Disk write avg", "Net RX avg", "Net TX avg"}
}
func (r performanceReport) Rows() [][]string {
	var out [][]string
	for _, c := range r.Computers {
		out = append(out, []string{c.Hostname, fmt.Sprint(c.Samples), fmt.Sprintf("%.1f", c.CPUAvg), fmt.Sprintf("%.1f", c.CPUMax),
			fmt.Sprintf("%.1f", c.RAMAvg), fmt.Sprintf("%.1f", c.RAMMax), rate(c.DiskReadAvg), rate(c.DiskWriteAvg), rate(c.NetRxAvg), rate(c.NetTxAvg)})
	}
	return out
}
func (r performanceReport) Sections() []reportSection {
	return []reportSection{{Heading: fmt.Sprintf("Period: %s to %s", r.From.Local().Format("2006-01-02 15:04"), r.To.Local().Format("2006-01-02 15:04"))}}
}
func (r performanceReport) WriteCSV(w io.Writer) error {
	for _, s := range r.Sections() {
		fmt.Fprintf(w, "# %s\n", s.Heading)
	}
	return writeCSV(w, r.Columns(), r.Rows())
}
func (r performanceReport) PDF() ([]byte, error) {
	return renderPDF(r.Title(), r.Sections(), r.Columns(), r.Rows(), len(r.Computers))
}
func (r performanceReport) HTML() string {
	return renderHTML(r.Title(), r.Sections(), r.Columns(), r.Rows())
}

func rate(bps float64) string {
	if bps < 1024 {
		return fmt.Sprintf("%.0f B/s", bps)
	}
	if bps < 1024*1024 {
		return fmt.Sprintf("%.1f KB/s", bps/1024)
	}
	if bps < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB/s", bps/1024/1024)
	}
	return fmt.Sprintf("%.2f GB/s", bps/1024/1024/1024)
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

var _ = bytes.MinRead
