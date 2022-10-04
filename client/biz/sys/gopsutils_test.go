package sys

import (
	"fmt"
	"testing"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/host"
	"github.com/shirou/gopsutil/mem"
)

// 磁盘信息
func TestDiskInfo(t *testing.T) {
	parts, err := disk.Partitions(true)
	if err != nil {
		fmt.Printf("获取磁盘信息失败 , err:%v\n", err)
		return
	}
	for _, part := range parts {
		fmt.Printf("磁盘分区 :%v\n", part.String())
		diskInfo, _ := disk.Usage(part.Mountpoint)
		fmt.Printf("该磁盘使用信息:used:%v free:%v\n", diskInfo.UsedPercent, diskInfo.Free)
	}

	ioStat, _ := disk.IOCounters()
	for k, v := range ioStat {
		fmt.Printf("%v:%v\n", k, v)
	}
}

//内存信息
func TestMemInfo(t *testing.T) {
	//获取内存信息
	memInfo, _ := mem.VirtualMemory()
	fmt.Printf("内存信息 :\n %v", memInfo)
}

//主机信息
func TestHostInfo(t *testing.T) {
	hInfo, _ := host.Info()
	fmt.Printf("主机信息 :\n %v", hInfo)
}

func TestCpuInfo(t *testing.T) {
	//1 CPU全部信息
	cpuInfos, err := cpu.Info()
	if err != nil {
		fmt.Printf("获取CPU信息出错 , err:\n %v", err)
	}
	for _, ci := range cpuInfos {
		fmt.Println("CPU基本信息 : \n", ci)
	}
	// 实时加载CPU使用率
	percent, _ := cpu.Percent(time.Second, true)
	fmt.Printf("CPU负载信息 一秒: %v\n", percent)

	//percent, _ = cpu.Percent(5*time.Minute, false)
	//fmt.Printf("CPU负载信息 五分钟: %v\n", percent)
	//
	//percent, _ = cpu.Percent(15*time.Minute, false)
	//fmt.Printf("CPU负载信息 15分钟: %v\n", percent)
}
