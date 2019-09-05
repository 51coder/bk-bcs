/*
Copyright (C) 2019 The BlueKing Authors. All rights reserved.

Permission is hereby granted, free of charge, to any person obtaining a copy of
this software and associated documentation files (the "Software"), to deal in
the Software without restriction, including without limitation the rights to
use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
of the Software, and to permit persons to whom the Software is furnished to do
so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package cache

import (
	"bk-bcs/bcs-common/common/blog"
	"bk-bcs/bcs-common/pkg/watch"
	"time"

	"golang.org/x/net/context"
)

//NewReflector create new reflector
func NewReflector(name string, store Store, lw ListerWatcher, fullSyncPeriod time.Duration, handler EventInterface) *Reflector {
	cxt, stopfn := context.WithCancel(context.Background())
	return &Reflector{
		name:       name,
		cxt:        cxt,
		stopFn:     stopfn,
		listWatch:  lw,
		syncPeriod: fullSyncPeriod,
		store:      store,
		handler:    handler,
		underWatch: false,
	}
}

//Reflector offers lister & watcher mechanism to sync event-storage data
//to local memory store, and meanwhile push data event to predifine event handler
type Reflector struct {
	name       string //reflector name
	cxt        context.Context
	stopFn     context.CancelFunc
	listWatch  ListerWatcher  //lister & watcher for object data
	syncPeriod time.Duration  //period for resync all data
	store      Store          //memory store for all data object
	handler    EventInterface //event callback when processing store data
	underWatch bool           //flag for watch handler
}

//Run running reflector, list all data in period and create stable watcher for
//all data events
func (r *Reflector) Run() {
	blog.Infof("%s ready to start, begin to cache data", r.name)
	//sync all data object from remote event storage
	r.listAllData()
	watchCxt, _ := context.WithCancel(r.cxt)
	go r.handleWatch(watchCxt)
	blog.Infof("%s first resynchronization & watch success, register all ticker", r.name)
	//create ticker for data object resync
	syncTick := time.NewTicker(r.syncPeriod)
	//create ticker check stable watcher
	watchTick := time.NewTicker(time.Second * 2)
	for {
		select {
		case <-r.cxt.Done():
			blog.Warnf("%s running exit, lister & watcher stopped", r.name)
			return
		case <-syncTick.C:
			//fully resync all datas in period
			blog.Infof("%s trigger all data synchronization", r.name)
			r.listAllData()
		case <-watchTick.C:
			//check watch is running
			if r.underWatch {
				continue
			}
			//watch is out, recovery watch loop
			blog.Warnf("%s long watch is out, start watch loop to recovery.", r.name)
			go r.handleWatch(watchCxt)
		}
	}
}

//Stop stop reflector
func (r *Reflector) Stop() {
	blog.V(3).Infof("%s is asked to stop", r.name)
	r.stopFn()
}

func (r *Reflector) listAllData() {
	blog.V(3).Infof("%s begins to list all data...", r.name)
	objs, err := r.listWatch.List()
	if err != nil {
		//some err response, wait for next resync ticker
		blog.Errorf("%s List all data failed, %s", r.name, err)
		return
	}
	blog.V(3).Infof("%s list all data success, objects number %d", r.name, len(objs))
	for _, obj := range objs {
		oldObj, exist, err := r.store.Get(obj)
		if err != nil {
			blog.Errorf("%s gets local store err under List, %s, discard data", r.name, err)
			continue
		}
		if exist {
			r.store.Update(obj)
			if r.handler != nil {
				r.handler.OnUpdate(oldObj, obj)
			}
			blog.V(5).Infof("%s update %s/%s notify succes in Lister.", r.name, obj.GetNamespace(), obj.GetName())
		} else {
			r.store.Add(obj)
			if r.handler != nil {
				r.handler.OnAdd(obj)
			}
			blog.V(5).Infof("%s add %s/%s notify succes in Lister.", r.name, obj.GetNamespace(), obj.GetName())
		}
	}
}

func (r *Reflector) handleWatch(cxt context.Context) {
	if r.underWatch {
		return
	}
	r.underWatch = true
	watcher, err := r.listWatch.Watch()
	if err != nil {
		blog.Errorf("Reflector %s create watch by ListerWatcher failed, %s", r.name, err)
		r.underWatch = false
		return
	}
	defer func() {
		r.underWatch = false
		watcher.Stop()
		blog.Infof("Reflector %s watch loop exit", r.name)
	}()
	blog.Infof("%s enter storage watch loop, waiting for event trigger", r.name)
	channel := watcher.WatchEvent()
	for {
		select {
		case <-cxt.Done():
			blog.Infof("reflector %s is asked to exit.", r.name)
			return
		case event, ok := <-channel:
			if !ok {
				blog.Errorf("%s reads watch.Event from channel failed. channel closed", r.name)
				return
			}
			switch event.Type {
			case watch.EventSync, watch.EventAdded, watch.EventUpdated:
				r.processAddUpdate(&event)
			case watch.EventDeleted:
				r.processDeletion(&event)
			case watch.EventErr:
				//some unexpected err occured, but channel & watach is still work
				blog.V(3).Infof("Reflector %s catch some data err in watch.Event channel, keep watch running", r.name)
			}
		}
	}
}

func (r *Reflector) processAddUpdate(event *watch.Event) {
	oldObj, exist, err := r.store.Get(event.Data)
	if err != nil {
		blog.V(3).Infof("Reflector %s gets local store err, %s", r.name, err)
		return
	}
	if exist {
		r.store.Update(event.Data)
		if r.handler != nil {
			r.handler.OnUpdate(oldObj, event.Data)
		}
	} else {
		r.store.Add(event.Data)
		if r.handler != nil {
			r.handler.OnAdd(event.Data)
		}
	}
}

func (r *Reflector) processDeletion(event *watch.Event) {
	//fix(DeveloperJim): 2018-06-26 16:42:10
	//when deletion happens in zookeeper, no Object dispatchs, so we
	//need to get object from local cache
	oldObj, exist, err := r.store.Get(event.Data)
	if err != nil {
		blog.V(3).Infof("Reflector %s gets local store err in DeleteEvent, %s", r.name, err)
		return
	}
	if exist {
		r.store.Delete(event.Data)
		if event.Data.GetAnnotations() != nil && event.Data.GetAnnotations()["bk-bcs-inner-storage"] == "bkbcs-zookeeper" {
			//tricky here, zookeeper can't get creation time when deletion
			if r.handler != nil {
				r.handler.OnDelete(oldObj)
			}
			blog.V(5).Infof("reflector %s invoke Delete tricky callback func for %s/%s.", r.name, event.Data.GetNamespace(), event.Data.GetName())
		} else {
			if r.handler != nil {
				r.handler.OnDelete(event.Data)
			}
			blog.V(5).Infof("reflector %s invoke Delete callback for %s/%s.", r.name, event.Data.GetNamespace(), event.Data.GetName())
		}
	}
	//local cache do not exist, nothing happens
	blog.Errorf("reflector %s lost local cache for %s/%s", r.name, event.Data.GetNamespace(), event.Data.GetName())
}

//EventInterface register interface for event notification
type EventInterface interface {
	OnAdd(obj interface{})
	OnUpdate(old, cur interface{})
	OnDelete(obj interface{})
}

//EventHandler reigster events call back for data change
type EventHandler struct {
	AddFn    func(obj interface{})
	UpdateFn func(old, cur interface{})
	DeleteFn func(obj interface{})
}

//OnAdd implements EventInterface
func (h *EventHandler) OnAdd(obj interface{}) {
	if h.AddFn == nil {
		return
	}
	h.AddFn(obj)
}

//OnUpdate implements EventInterface
func (h *EventHandler) OnUpdate(old, cur interface{}) {
	if h.UpdateFn == nil {
		return
	}
	h.UpdateFn(old, cur)
}

//OnDelete implements EventInterface
func (h *EventHandler) OnDelete(obj interface{}) {
	if h.DeleteFn == nil {
		return
	}
	h.DeleteFn(obj)
}
