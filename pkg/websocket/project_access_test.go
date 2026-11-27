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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessCache(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	newCache := func() *accessCache {
		cache := newAccessCache(30 * time.Second)
		cache.now = func() time.Time { return now }
		return cache
	}
	counting := func(verdict bool) (func() (bool, error), *int) {
		calls := 0
		return func() (bool, error) {
			calls++
			return verdict, nil
		}, &calls
	}

	t.Run("asks once within the ttl", func(t *testing.T) {
		cache := newCache()
		check, calls := counting(true)

		for range 3 {
			allowed, err := cache.allowed(1, 7, check)
			require.NoError(t, err)
			assert.True(t, allowed)
		}

		assert.Equal(t, 1, *calls)
	})

	t.Run("asks again once the entry expired", func(t *testing.T) {
		cache := newCache()
		check, calls := counting(false)
		_, err := cache.allowed(1, 7, check)
		require.NoError(t, err)

		now = now.Add(31 * time.Second)
		allowed, err := cache.allowed(1, 7, check)
		require.NoError(t, err)

		assert.False(t, allowed)
		assert.Equal(t, 2, *calls)
	})

	t.Run("set overrides a cached verdict", func(t *testing.T) {
		cache := newCache()
		denied, _ := counting(false)
		_, err := cache.allowed(1, 7, denied)
		require.NoError(t, err)

		cache.set(1, 7, true)
		allowed, err := cache.allowed(1, 7, denied)
		require.NoError(t, err)

		assert.True(t, allowed)
	})

	t.Run("a failing check is not cached", func(t *testing.T) {
		cache := newCache()
		calls := 0
		failing := func() (bool, error) {
			calls++
			return false, errors.New("db down")
		}

		_, err := cache.allowed(1, 7, failing)
		require.Error(t, err)
		_, err = cache.allowed(1, 7, failing)
		require.Error(t, err)

		assert.Equal(t, 2, calls)
	})

	t.Run("keys by user and project", func(t *testing.T) {
		cache := newCache()
		cache.set(1, 7, true)
		denied, calls := counting(false)

		allowed, err := cache.allowed(2, 7, denied)
		require.NoError(t, err)
		assert.False(t, allowed)
		allowed, err = cache.allowed(1, 8, denied)
		require.NoError(t, err)
		assert.False(t, allowed)
		assert.Equal(t, 2, *calls)
	})
}
