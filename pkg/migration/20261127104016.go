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

package migration

import (
	"time"

	"src.techknowlogick.com/xormigrate"
	"xorm.io/xorm"
)

type taskQuestions20261127104016 struct {
	ID             int64             `xorm:"bigint autoincr not null unique pk"`
	TaskID         int64             `xorm:"bigint not null index"`
	AuthorID       int64             `xorm:"bigint not null"`
	Title          string            `xorm:"text not null"`
	Summary        string            `xorm:"text null"`
	Details        string            `xorm:"text null"`
	Options        []map[string]any  `xorm:"json null"`
	Recommendation map[string]string `xorm:"json null"`
	Status         string            `xorm:"varchar(20) not null default 'open' index"`
	AnswerOption   string            `xorm:"varchar(20) null"`
	AnswerOther    string            `xorm:"text null"`
	AnswerNotes    string            `xorm:"text null"`
	AnsweredByID   int64             `xorm:"bigint null"`
	AnsweredAt     *time.Time        `xorm:"datetime null"`
	Created        time.Time         `xorm:"created not null"`
	Updated        time.Time         `xorm:"updated not null"`
}

func (taskQuestions20261127104016) TableName() string {
	return "task_questions"
}

func createTaskQuestions20261127104016(tx *xorm.Engine) error {
	return tx.Sync(taskQuestions20261127104016{}) //nolint:forbidigo // brand-new table, nothing to drop
}

func init() {
	migrations = append(migrations, &xormigrate.Migration{
		ID:          "20261127104016",
		Description: "Add task_questions table for questions asked on a task",
		Migrate:     createTaskQuestions20261127104016,
		Rollback: func(tx *xorm.Engine) error {
			return tx.DropTables(taskQuestions20261127104016{})
		},
	})
}
