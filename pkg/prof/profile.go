// Package prof is wrap the official `net/http/pprof` route and add the profile io wait time route.
package prof

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

var (
	durationSecond  uint32 = 60
	isSamplingTrace        = false

	serverName = getServerName()
	pid        = syscall.Getpid()
	timeFormat = "20060102T150405"

	status      uint32
	statusStart uint32 = 1 // status=1
	statusStop  uint32     // status=0
)

// WaitSign wait system notification signals
//func WaitSign() {
//	p := NewProfile()
//
//	signals := make(chan os.Signal, 1)
//	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGTRAP)
//
//	for {
//		v := <-signals
//		switch v {
//		case syscall.SIGTRAP:
//			p.StartOrStop()
//
//		case syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP:
//			gracefully exit
//			os.Exit(0)
//		}
//	}
//}

// Profile represents a profiling session
type Profile struct {
	files    []string
	closeFns []func()

	//ctx    context.Context
	stopCh chan struct{}
}

// NewProfile create a new profile
func NewProfile() *Profile {
	p := new(Profile)
	p.stopCh = make(chan struct{})
	return p
}

// StartOrStop start and stop sampling profile, the first call to start sampling data, the default maximum is 60 seconds,
// in less than 60s, if the second execution will actively stop sampling profile
func (p *Profile) StartOrStop() {
	if isStart() {
		p.startProfile()
	} else if isStop() {
		p.stopProfile()
	}
}

func (p *Profile) startProfile() {
	fmt.Printf("[profile] start sampling profile, status=%d\n", status)

	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()

	err := p.cpu()
	if err != nil {
		fmt.Println(err)
	}

	err = p.mem()
	if err != nil {
		fmt.Println(err)
	}

	err = p.goroutine()
	if err != nil {
		fmt.Println(err)
	}

	err = p.block()
	if err != nil {
		fmt.Println(err)
	}

	err = p.mutex()
	if err != nil {
		fmt.Println(err)
	}

	err = p.threadCreate()
	if err != nil {
		fmt.Println(err)
	}

	if isSamplingTrace {
		err = p.tracing()
		if err != nil {
			fmt.Println(err)
		}
	}

	go p.checkTimeout()
}

func (p *Profile) stopProfile() {
	fmt.Printf("[profile] stop sampling profile, status=%d\n", status)

	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()

	if p == nil || len(p.closeFns) == 0 {
		return
	}

	for _, fn := range p.closeFns {
		fn()
	}

	select {
	case p.stopCh <- struct{}{}:
	default:
	}

	// reset profile
	p = NewProfile() //nolint
}

func (p *Profile) checkTimeout() {
	if p == nil {
		return
	}

	ctx, _ := context.WithTimeout(context.Background(), time.Second*time.Duration(durationSecond)) //nolint
	select {
	case <-p.stopCh:
		fmt.Println("[profile] stop collecting profiles: manual")
		return
	case <-ctx.Done():
		if isStop() {
			p.stopProfile()
		}
		fmt.Println("[profile] stop collecting profiles: time is up")
	}
}

func (p *Profile) cpu() error {
	profileName := "cpu"
	file := getFilePath(profileName)
	f, err := os.Create(file)
	if err != nil {
		return err
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return err
	}

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		pprof.StopCPUProfile()
		_ = f.Close()
	})

	return nil
}

func (p *Profile) mem() error {
	profileName := "mem"
	file := getFilePath(profileName)
	f, err := os.Create(file)
	if err != nil {
		return err
	}

	old := runtime.MemProfileRate
	runtime.MemProfileRate = 4096

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
			fmt.Printf("write mem profile error: %v\n", err)
		}
		_ = f.Close()
		runtime.MemProfileRate = old
	})

	return nil
}

func (p *Profile) goroutine() error {
	profileName := "goroutine"
	file := getFilePath(profileName)
	f, err := os.Create(file)
	if err != nil {
		return err
	}

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if err := pprof.Lookup(profileName).WriteTo(f, 0); err != nil {
			fmt.Printf("write goroutine profile error: %v\n", err)
		}
		_ = f.Close()
	})

	return nil
}

func (p *Profile) block() error {
	profileName := "block"
	file := getFilePath(profileName)
	f, err := os.Create(file)
	if err != nil {
		return err
	}

	runtime.SetBlockProfileRate(1)

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if err := pprof.Lookup(profileName).WriteTo(f, 0); err != nil {
			fmt.Printf("write block profile error: %v\n", err)
		}
		_ = f.Close()
		runtime.SetBlockProfileRate(0)
	})

	return nil
}

func (p *Profile) mutex() error {
	profileName := "mutex"
	file := getFilePath(profileName)
	f, err := os.Create(file)
	if err != nil {
		return err
	}

	runtime.SetMutexProfileFraction(1)

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if mp := pprof.Lookup(profileName); mp != nil {
			if err := mp.WriteTo(f, 0); err != nil {
				fmt.Printf("write mutex profile error: %v\n", err)
			}
		}
		_ = f.Close()
		runtime.SetMutexProfileFraction(0)
	})

	return nil
}

func (p *Profile) threadCreate() error {
	profileName := "threadcreate"
	file := getFilePath(profileName)
	f, err := os.Create(file)
	if err != nil {
		return err
	}

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		if mp := pprof.Lookup(profileName); mp != nil {
			if err := mp.WriteTo(f, 0); err != nil {
				fmt.Printf("write threadcreate profile error: %v\n", err)
			}
		}
		_ = f.Close()
	})

	return nil
}

func (p *Profile) tracing() error {
	profileName := "trace"
	file := getFilePath(profileName)
	f, err := os.Create(file)
	if err != nil {
		return err
	}

	err = trace.Start(f)
	if err != nil {
		_ = f.Close()
		return err
	}

	p.files = append(p.files, file)
	p.closeFns = append(p.closeFns, func() {
		trace.Stop()
		_ = f.Close()
	})

	return nil
}

// SetDurationSecond set sampling profile duration
func SetDurationSecond(d uint32) {
	atomic.StoreUint32(&durationSecond, d)
}

// EnableTrace enable sampling trace profile
func EnableTrace() {
	isSamplingTrace = true
}

func isStart() bool {
	return atomic.CompareAndSwapUint32(&status, statusStop, statusStart)
}

func isStop() bool {
	return atomic.CompareAndSwapUint32(&status, statusStart, statusStop)
}

func getFilePath(profileName string) string {
	dir := joinPath(os.TempDir(), serverName+"_profile")
	if err := os.MkdirAll(dir, 0766); err != nil {
		fmt.Printf("create profile directory error: %v\n", err)
	}

	return joinPath(dir, fmt.Sprintf("%s_%d_%s_%s.out",
		time.Now().Format(timeFormat), pid, serverName, profileName))
}

func getServerName() string {
	_, name := filepath.Split(os.Args[0])
	return strings.TrimSuffix(name, path.Ext(name))
}

func joinPath(elem ...string) string {
	dir := strings.Join(elem, "/")
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(dir, "/", "\\")
	}
	return dir
}
