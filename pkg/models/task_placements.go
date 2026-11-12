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

package models

import (
	"xorm.io/xorm"
)

// TaskPlacement is where a task sits in one view of its project: its kanban
// bucket and its sort position. Both come from the stored rows, so a view
// without a row for the task is left out and the task falls back to the
// view's default bucket and the unpositioned order, as the task listing does.
type TaskPlacement struct {
	ProjectViewID int64   `json:"project_view_id"`
	BucketID      int64   `json:"bucket_id,omitempty"`
	Position      float64 `json:"position,omitempty"`
}

// GetTaskPlacements returns the stored bucket and position of a task in every
// view of projectID, in view order. Views of other projects are ignored even
// when the task still has rows there.
func GetTaskPlacements(s *xorm.Session, taskID, projectID int64) ([]*TaskPlacement, error) {
	views, err := getViewsForProject(s, projectID)
	if err != nil {
		return nil, err
	}
	if len(views) == 0 {
		return []*TaskPlacement{}, nil
	}

	viewIDs := make([]int64, 0, len(views))
	for _, view := range views {
		viewIDs = append(viewIDs, view.ID)
	}

	buckets := []*TaskBucket{}
	err = s.Where("task_id = ?", taskID).In("project_view_id", viewIDs).Find(&buckets)
	if err != nil {
		return nil, err
	}
	positions := []*TaskPosition{}
	err = s.Where("task_id = ?", taskID).In("project_view_id", viewIDs).Find(&positions)
	if err != nil {
		return nil, err
	}

	byView := make(map[int64]*TaskPlacement, len(views))
	placementFor := func(viewID int64) *TaskPlacement {
		if placement, has := byView[viewID]; has {
			return placement
		}
		placement := &TaskPlacement{ProjectViewID: viewID}
		byView[viewID] = placement
		return placement
	}
	for _, bucket := range buckets {
		placementFor(bucket.ProjectViewID).BucketID = bucket.BucketID
	}
	for _, position := range positions {
		placementFor(position.ProjectViewID).Position = position.Position
	}

	placements := make([]*TaskPlacement, 0, len(byView))
	for _, view := range views {
		if placement, has := byView[view.ID]; has {
			placements = append(placements, placement)
		}
	}
	return placements, nil
}
