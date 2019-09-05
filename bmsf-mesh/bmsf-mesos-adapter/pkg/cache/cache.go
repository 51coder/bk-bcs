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
	"sync"
)

//NewCache create cache with designated ObjectKeyFunc
func NewCache(kfunc ObjectKeyFunc) Store {
	return &Cache{
		dataMap: make(map[string]interface{}),
		keyFunc: kfunc,
	}
}

//CreateCache create cache with designated ObjectKeyFunc
func CreateCache(kfunc ObjectKeyFunc) *Cache {
	return &Cache{
		dataMap: make(map[string]interface{}),
		keyFunc: kfunc,
	}
}

//CreateCache create Cache object
// func CreateCache(kfunc ObjectKeyFunc) *Cache {
// 	return &Cache{
// 		dataMap: make(map[string]interface{}),
// 		keyFunc: kfunc,
// 	}
// }

//Cache implements Store interface with a safe map
type Cache struct {
	lock    sync.RWMutex           //lock for datamap
	dataMap map[string]interface{} //map to hold data
	keyFunc ObjectKeyFunc          //func to create key
}

// Add inserts an item into the cache.
func (c *Cache) Add(obj interface{}) error {
	key, err := c.keyFunc(obj)
	if err != nil {
		return KeyError{obj, err}
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	c.dataMap[key] = obj
	return nil
}

// Update sets an item in the cache to its updated state.
func (c *Cache) Update(obj interface{}) error {
	key, err := c.keyFunc(obj)
	if err != nil {
		return KeyError{obj, err}
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	c.dataMap[key] = obj
	return nil
}

// Delete removes an item from the cache.
func (c *Cache) Delete(obj interface{}) error {
	key, err := c.keyFunc(obj)
	if err != nil {
		return KeyError{obj, err}
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	if _, found := c.dataMap[key]; found {
		delete(c.dataMap, key)
	} else {
		return DataNoExist{obj}
	}
	return nil
}

// Get returns the requested item, or sets exists=false.
// Get is completely threadsafe as long as you treat all items as immutable.
func (c *Cache) Get(obj interface{}) (item interface{}, exists bool, err error) {
	key, err := c.keyFunc(obj)
	if err != nil {
		return nil, false, KeyError{obj, err}
	}
	c.lock.RLock()
	defer c.lock.RUnlock()
	item, exists = c.dataMap[key]
	return item, exists, nil
}

// List returns a list of all the items.
// List is completely threadsafe as long as you treat all items as immutable.
func (c *Cache) List() []interface{} {
	c.lock.RLock()
	defer c.lock.RUnlock()
	data := make([]interface{}, 0, len(c.dataMap))
	for _, item := range c.dataMap {
		data = append(data, item)
	}
	return data
}

// ListKeys returns a list of all the keys of the objects currently
// in the cache.
func (c *Cache) ListKeys() []string {
	c.lock.RLock()
	defer c.lock.RUnlock()
	list := make([]string, 0, len(c.dataMap))
	for key := range c.dataMap {
		list = append(list, key)
	}
	return list
}

// GetByKey returns the request item, or exists=false.
// GetByKey is completely threadsafe as long as you treat all items as immutable.
func (c *Cache) GetByKey(key string) (item interface{}, exists bool, err error) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	item, exists = c.dataMap[key]
	return item, exists, nil
}

//Num will return data counts in Store
func (c *Cache) Num() int {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return len(c.dataMap)
}

//Clear will drop all data in Store
func (c *Cache) Clear() {
	c.lock.Lock()
	defer c.lock.Unlock()
	for key := range c.dataMap {
		delete(c.dataMap, key)
	}
}

// Replace will delete the contents of 'c', using instead the given list.
// 'c' takes ownership of the list, you should not reference the list again
// after calling this function.
func (c *Cache) Replace(list []interface{}) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	for _, item := range list {
		key, err := c.keyFunc(item)
		if err != nil {
			return KeyError{item, err}
		}
		c.dataMap[key] = item
	}
	return nil
}

//Reset clean data first and then setting data
func (c *Cache) Reset(list []interface{}) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.dataMap = make(map[string]interface{})
	for _, item := range list {
		key, err := c.keyFunc(item)
		if err != nil {
			return KeyError{item, err}
		}
		c.dataMap[key] = item
	}
	return nil
}
