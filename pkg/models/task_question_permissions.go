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
	"code.vikunja.io/api/pkg/web"

	"xorm.io/xorm"
)

// CanRead checks read access to the question's task. It loads the question,
// scoped to the task from the path.
func (q *TaskQuestion) CanRead(s *xorm.Session, a web.Auth) (bool, int, error) {
	if err := getTaskQuestionSimple(s, q); err != nil {
		return false, 0, err
	}
	t := Task{ID: q.TaskID}
	return t.CanRead(s, a)
}

// canWriteTaskQuestion checks write access to the task of a stored question.
func (q *TaskQuestion) canWriteTaskQuestion(s *xorm.Session, a web.Auth) (bool, error) {
	saved := &TaskQuestion{ID: q.ID, TaskID: q.TaskID}
	if err := getTaskQuestionSimple(s, saved); err != nil {
		return false, err
	}
	q.TaskID = saved.TaskID
	t := Task{ID: saved.TaskID}
	return t.CanWrite(s, a)
}

// CanCreate checks write access to the task. Anyone who can edit the task may ask.
func (q *TaskQuestion) CanCreate(s *xorm.Session, a web.Auth) (bool, error) {
	t := Task{ID: q.TaskID}
	return t.CanWrite(s, a)
}

// CanUpdate checks write access to the task. It also gates answering.
func (q *TaskQuestion) CanUpdate(s *xorm.Session, a web.Auth) (bool, error) {
	return q.canWriteTaskQuestion(s, a)
}

// CanDelete checks write access to the task.
func (q *TaskQuestion) CanDelete(s *xorm.Session, a web.Auth) (bool, error) {
	return q.canWriteTaskQuestion(s, a)
}
