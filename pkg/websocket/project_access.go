// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package websocket

import (
	"sync"
	"time"
)

// projectAccessTTL bounds how long a user who lost access to a project keeps
// receiving its events: nothing fires when a share is removed, so delivery
// re-checks read access with this cache in front of the database. The
// payload carries ids only, so the worst case inside the window is learning
// that some task of the project changed.
const projectAccessTTL = 30 * time.Second

// accessCachePruneSize is the entry count above which a lookup sweeps expired entries.
const accessCachePruneSize = 1024

type accessKey struct{ userID, projectID int64 }

type accessEntry struct {
	allowed bool
	expires time.Time
}

// accessCache memoizes read verdicts per user and project for a short time.
type accessCache struct {
	mu      sync.Mutex
	entries map[accessKey]accessEntry
	ttl     time.Duration
	now     func() time.Time
}

var projectAccess = newAccessCache(projectAccessTTL)

func newAccessCache(ttl time.Duration) *accessCache {
	return &accessCache{
		entries: make(map[accessKey]accessEntry),
		ttl:     ttl,
		now:     time.Now,
	}
}

// set records a verdict, replacing whatever was cached for the pair.
func (c *accessCache) set(userID, projectID int64, allowed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[accessKey{userID, projectID}] = accessEntry{allowed: allowed, expires: c.now().Add(c.ttl)}
}

// allowed returns the cached verdict for the pair, asking check for a fresh
// one when none is cached or it expired. A failing check denies without
// caching, so the next event asks again.
func (c *accessCache) allowed(userID, projectID int64, check func() (bool, error)) (bool, error) {
	key := accessKey{userID, projectID}
	now := c.now()

	c.mu.Lock()
	entry, cached := c.entries[key]
	if cached && now.Before(entry.expires) {
		c.mu.Unlock()
		return entry.allowed, nil
	}
	if len(c.entries) > accessCachePruneSize {
		for k, e := range c.entries {
			if !now.Before(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	c.mu.Unlock()

	allowed, err := check()
	if err != nil {
		return false, err
	}
	c.set(userID, projectID, allowed)
	return allowed, nil
}

func (c *accessCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[accessKey]accessEntry)
}
