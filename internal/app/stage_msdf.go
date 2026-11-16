package app

import (
	"github.com/egoist/mygo/ui"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// This one-shot handoff owns an engine only before UI publication. Shutdown
// can abandon it without letting the loader publish into a closed window.
type stageMSDFLoad struct {
	mu        sync.Mutex
	abandoned bool
	engine    *ui.DynamicMSDF
	err       error
}

func (l *stageMSDFLoad) store(d *ui.DynamicMSDF, e error) bool {
	l.mu.Lock()
	if l.abandoned {
		l.mu.Unlock()
		if d != nil {
			d.Close()
		}
		return false
	}
	l.engine, l.err = d, e
	l.mu.Unlock()
	return true
}
func (l *stageMSDFLoad) take() (*ui.DynamicMSDF, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	d, e := l.engine, l.err
	l.engine, l.err = nil, nil
	return d, e
}
func (l *stageMSDFLoad) cancel() {
	l.mu.Lock()
	l.abandoned = true
	d := l.engine
	l.engine, l.err = nil, nil
	l.mu.Unlock()
	if d != nil {
		d.Close()
	}
}

// Loading/verification runs off paint once per process. The only cross-thread
// state is the locked unpublished handoff; app state is read/written on UI.
func (a *app) initDynamicMSDF() {
	if a.stageDynamicLoading || a.stageDynamic != nil || a.win == nil || a.quitting {
		return
	}
	a.stageDynamicLoading = true
	load := &stageMSDFLoad{}
	a.stageDynamicLoad = load
	win := a.win
	go func() {
		exe, e := os.Executable()
		var d *ui.DynamicMSDF
		if e == nil {
			d, e = ui.OpenDynamicMSDF(filepath.Join(filepath.Dir(exe), "cloudy_mtsdf.dll"), stageDistanceDLLSHA, func(fn func()) { win.Update(fn) })
		}
		if !load.store(d, e) {
			return
		}
		win.Update(func() {
			d, e := load.take()
			if a.quitting {
				if d != nil {
					d.Close()
				}
				return
			}
			if e != nil {
				log.Println("高清动态文字暂不可用，已使用原生字体：", e)
				return
			}
			if d == nil {
				return
			}
			a.stageDynamic = d
			if a.settings.StageTextRender != "msdf" {
				d.Suspend()
			}
			a.redraw()
		})
	}()
}
func (a *app) closeDynamicMSDF() {
	if a.stageDynamicLoad != nil {
		a.stageDynamicLoad.cancel()
	}
	if a.stageDynamic != nil {
		a.stageDynamic.Close()
		a.stageDynamic = nil
	}
}
