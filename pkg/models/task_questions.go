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
	"time"

	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/api/pkg/web"

	"xorm.io/builder"
	"xorm.io/xorm"
)

// TaskQuestionStatus is the state of a task question.
type TaskQuestionStatus string

const (
	TaskQuestionStatusOpen      TaskQuestionStatus = "open"
	TaskQuestionStatusAnswered  TaskQuestionStatus = "answered"
	TaskQuestionStatusDeferred  TaskQuestionStatus = "deferred"
	TaskQuestionStatusCancelled TaskQuestionStatus = "cancelled"
)

func (s TaskQuestionStatus) valid() bool {
	switch s {
	case TaskQuestionStatusOpen, TaskQuestionStatusAnswered, TaskQuestionStatusDeferred, TaskQuestionStatusCancelled:
		return true
	}
	return false
}

// TaskQuestionOption is one of the choices a question offers.
type TaskQuestionOption struct {
	Key         string `json:"key" minLength:"1" maxLength:"20" doc:"Short, unique key of the option within the question, e.g. A, B, C. Answers refer to it."`
	Label       string `json:"label" minLength:"1" maxLength:"250" doc:"One-line label shown on the option's button."`
	Description string `json:"description,omitempty" doc:"Optional longer explanation of the option."`
}

// TaskQuestionRecommendation is the option the asker would pick.
type TaskQuestionRecommendation struct {
	Key    string `json:"key" doc:"Key of the recommended option. Must be one of the question's option keys."`
	Reason string `json:"reason,omitempty" doc:"Why this option is recommended."`
}

// TaskQuestion is a decision someone asks on a task, answered by picking an
// option, writing another answer, or deferring it.
type TaskQuestion struct {
	ID       int64      `xorm:"bigint autoincr not null unique pk" json:"id" param:"question" readOnly:"true" doc:"The unique, numeric id of this question."`
	TaskID   int64      `xorm:"bigint not null index" json:"task_id" param:"task" readOnly:"true" doc:"The task this question belongs to. Taken from the URL."`
	AuthorID int64      `xorm:"bigint not null" json:"-"`
	Author   *user.User `xorm:"-" json:"author" readOnly:"true" doc:"The user who asked the question. Set from the authenticated user on create."`

	Title          string                      `xorm:"text not null" json:"title" valid:"required" minLength:"1" doc:"The question, in one line."`
	Summary        string                      `xorm:"text null" json:"summary" doc:"A short summary shown under the title."`
	Details        string                      `xorm:"text null" json:"details" doc:"Longer context as HTML, collapsed by default in the UI."`
	Options        []*TaskQuestionOption       `xorm:"json null" json:"options" doc:"The choices. Keys must be unique and non-empty. A question without options can only be answered with other."`
	Recommendation *TaskQuestionRecommendation `xorm:"json null" json:"recommendation,omitempty" doc:"The option the asker recommends, with a reason."`

	Status TaskQuestionStatus `xorm:"varchar(20) not null default 'open' index" json:"status" enum:"open,answered,deferred,cancelled" doc:"open until answered or deferred through the answer endpoint. Updates may only set open (reopen) or cancelled."`

	AnswerOption string     `xorm:"varchar(20) null" json:"answer_option" readOnly:"true" doc:"Key of the chosen option, empty when the answer is other or the question was deferred."`
	AnswerOther  string     `xorm:"text null" json:"answer_other" readOnly:"true" doc:"Free-text answer given instead of an option."`
	AnswerNotes  string     `xorm:"text null" json:"answer_notes" readOnly:"true" doc:"Optional comment that came with the answer."`
	AnsweredByID int64      `xorm:"bigint null" json:"-"`
	AnsweredBy   *user.User `xorm:"-" json:"answered_by" readOnly:"true" doc:"Who answered or deferred the question."`
	AnsweredAt   *time.Time `xorm:"datetime null" json:"answered_at" readOnly:"true" doc:"When the question was answered or deferred."`

	Created time.Time `xorm:"created not null" json:"created" readOnly:"true" doc:"A timestamp when this question was created. You cannot change this value."`
	Updated time.Time `xorm:"updated not null" json:"updated" readOnly:"true" doc:"A timestamp when this question was last changed. You cannot change this value."`

	web.CRUDable    `xorm:"-" json:"-"`
	web.Permissions `xorm:"-" json:"-"`
}

// TableName holds the table name for task questions.
func (*TaskQuestion) TableName() string {
	return "task_questions"
}

// TaskQuestionAnswer is the body of the answer endpoint.
type TaskQuestionAnswer struct {
	Option string `json:"option,omitempty" doc:"Key of the chosen option. Leave empty when answering with other or deferring."`
	Other  string `json:"other,omitempty" doc:"Free-text answer instead of an option. Exclusive with option."`
	Notes  string `json:"notes,omitempty" doc:"Optional comment, allowed with any answer and with defer."`
	Defer  bool   `json:"defer,omitempty" doc:"Defer the question instead of answering it. Exclusive with option and other."`
}

func (q *TaskQuestion) optionKeys() map[string]bool {
	keys := make(map[string]bool, len(q.Options))
	for _, o := range q.Options {
		keys[o.Key] = true
	}
	return keys
}

func (q *TaskQuestion) validate() error {
	seen := map[string]bool{}
	for _, o := range q.Options {
		if o == nil || o.Key == "" || o.Label == "" {
			return ErrInvalidTaskQuestion{Reason: "every option needs a key and a label"}
		}
		if seen[o.Key] {
			return ErrInvalidTaskQuestion{Reason: "option keys must be unique, " + o.Key + " is repeated"}
		}
		seen[o.Key] = true
	}
	if q.Recommendation != nil {
		if q.Recommendation.Key == "" {
			q.Recommendation = nil
		} else if !seen[q.Recommendation.Key] {
			return ErrInvalidTaskQuestion{Reason: "the recommendation must name one of the option keys"}
		}
	}
	return nil
}

// Create asks a new question on a task.
func (q *TaskQuestion) Create(s *xorm.Session, a web.Auth) (err error) {
	if err := q.validate(); err != nil {
		return err
	}

	task, err := lockTaskForWrite(s, a, q.TaskID)
	if err != nil {
		return err
	}

	q.ID = 0
	q.Status = TaskQuestionStatusOpen
	q.clearAnswer()
	q.Author, err = GetUserOrLinkShareUser(s, a)
	if err != nil {
		return err
	}
	q.AuthorID = q.Author.ID

	if _, err = s.Insert(q); err != nil {
		return err
	}

	events.DispatchOnCommit(s, &TaskQuestionCreatedEvent{
		Task:     &task,
		Question: q,
		Doer:     q.Author,
	})
	return nil
}

func (q *TaskQuestion) clearAnswer() {
	q.AnswerOption = ""
	q.AnswerOther = ""
	q.AnswerNotes = ""
	q.AnsweredByID = 0
	q.AnsweredBy = nil
	q.AnsweredAt = nil
}

func getTaskQuestionSimple(s *xorm.Session, q *TaskQuestion) error {
	cond := builder.Eq{"id": q.ID}
	if q.TaskID != 0 {
		cond["task_id"] = q.TaskID
	}
	exists, err := s.Where(cond).NoAutoCondition().Get(q)
	if err != nil {
		return err
	}
	if !exists {
		return ErrTaskQuestionDoesNotExist{ID: q.ID, TaskID: q.TaskID}
	}
	return nil
}

// ReadOne returns a single question. CanRead has already loaded it.
func (q *TaskQuestion) ReadOne(s *xorm.Session, _ web.Auth) error {
	return addUsersToTaskQuestions(s, []*TaskQuestion{q})
}

// ReadAll returns the questions of a task, open ones first, then oldest first.
func (q *TaskQuestion) ReadAll(s *xorm.Session, a web.Auth, _ string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {
	t := &Task{ID: q.TaskID}
	canRead, _, err := t.CanRead(s, a)
	if err != nil {
		return nil, 0, 0, err
	}
	if !canRead {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	cond := builder.Eq{"task_id": q.TaskID}
	if q.Status != "" {
		cond["status"] = q.Status
	}

	questions := []*TaskQuestion{}
	limit, start := getLimitFromPageIndex(page, perPage)
	query := s.Where(cond).
		OrderBy("CASE WHEN status = 'open' THEN 0 ELSE 1 END, created ASC, id ASC")
	if limit > 0 {
		query = query.Limit(limit, start)
	}
	if err = query.Find(&questions); err != nil {
		return nil, 0, 0, err
	}

	if err = addUsersToTaskQuestions(s, questions); err != nil {
		return nil, 0, 0, err
	}

	numberOfTotalItems, err = s.Where(cond).Count(&TaskQuestion{})
	return questions, len(questions), numberOfTotalItems, err
}

func addUsersToTaskQuestions(s *xorm.Session, questions []*TaskQuestion) error {
	ids := []int64{}
	for _, q := range questions {
		ids = append(ids, q.AuthorID)
		if q.AnsweredByID != 0 {
			ids = append(ids, q.AnsweredByID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	users, err := getUsersOrLinkSharesFromIDs(s, ids)
	if err != nil {
		return err
	}
	for _, q := range questions {
		q.Author = users[q.AuthorID]
		if q.AnsweredByID != 0 {
			q.AnsweredBy = users[q.AnsweredByID]
		}
	}
	return nil
}

// Update changes the question's text, options or recommendation, or reopens or
// cancels it. Answers go through Answer.
func (q *TaskQuestion) Update(s *xorm.Session, a web.Auth) error {
	saved := &TaskQuestion{ID: q.ID, TaskID: q.TaskID}
	if err := getTaskQuestionSimple(s, saved); err != nil {
		return err
	}
	if _, err := lockTaskForWrite(s, a, saved.TaskID); err != nil {
		return err
	}
	if err := q.validate(); err != nil {
		return err
	}

	cols := []string{"title", "summary", "details", "options", "recommendation", "status"}
	switch q.Status {
	case "", saved.Status:
		q.Status = saved.Status
	case TaskQuestionStatusOpen:
		// Reopening drops the old answer.
		q.clearAnswer()
		cols = append(cols, "answer_option", "answer_other", "answer_notes", "answered_by_id", "answered_at")
	case TaskQuestionStatusCancelled:
	case TaskQuestionStatusAnswered, TaskQuestionStatusDeferred:
		return ErrInvalidTaskQuestion{Reason: "status can only be set to open or cancelled; answer through the answer endpoint"}
	default:
		return ErrInvalidTaskQuestion{Reason: "unknown status " + string(q.Status)}
	}

	if _, err := s.ID(q.ID).Cols(cols...).Update(q); err != nil {
		return err
	}

	*q = mergeTaskQuestion(saved, q, cols)
	return addUsersToTaskQuestions(s, []*TaskQuestion{q})
}

// mergeTaskQuestion returns the stored question with the updated columns applied.
func mergeTaskQuestion(saved, updated *TaskQuestion, cols []string) TaskQuestion {
	out := *saved
	for _, c := range cols {
		switch c {
		case "title":
			out.Title = updated.Title
		case "summary":
			out.Summary = updated.Summary
		case "details":
			out.Details = updated.Details
		case "options":
			out.Options = updated.Options
		case "recommendation":
			out.Recommendation = updated.Recommendation
		case "status":
			out.Status = updated.Status
		case "answer_option":
			out.clearAnswer()
		}
	}
	out.Updated = time.Now()
	return out
}

// Delete removes a question.
func (q *TaskQuestion) Delete(s *xorm.Session, a web.Auth) error {
	if err := getTaskQuestionSimple(s, q); err != nil {
		return err
	}
	if _, err := lockTaskForWrite(s, a, q.TaskID); err != nil {
		return err
	}
	_, err := s.ID(q.ID).NoAutoCondition().Delete(&TaskQuestion{})
	return err
}

// Answer answers or defers an open or deferred question. The caller must have
// checked CanUpdate.
func (q *TaskQuestion) Answer(s *xorm.Session, a web.Auth, answer *TaskQuestionAnswer) error {
	if err := getTaskQuestionSimple(s, q); err != nil {
		return err
	}
	task, err := lockTaskForWrite(s, a, q.TaskID)
	if err != nil {
		return err
	}
	if q.Status != TaskQuestionStatusOpen && q.Status != TaskQuestionStatusDeferred {
		return ErrTaskQuestionNotOpen{ID: q.ID, Status: q.Status}
	}

	given := 0
	for _, set := range []bool{answer.Option != "", answer.Other != "", answer.Defer} {
		if set {
			given++
		}
	}
	if given != 1 {
		return ErrInvalidTaskQuestionAnswer{Reason: "give exactly one of option, other or defer"}
	}
	if answer.Option != "" && !q.optionKeys()[answer.Option] {
		return ErrInvalidTaskQuestionAnswer{Reason: "option " + answer.Option + " is not one of the question's options"}
	}

	doer, err := GetUserOrLinkShareUser(s, a)
	if err != nil {
		return err
	}
	now := time.Now()
	q.Status = TaskQuestionStatusAnswered
	if answer.Defer {
		q.Status = TaskQuestionStatusDeferred
	}
	q.AnswerOption = answer.Option
	q.AnswerOther = answer.Other
	q.AnswerNotes = answer.Notes
	q.AnsweredByID = doer.ID
	q.AnsweredAt = &now

	if _, err := s.ID(q.ID).
		Cols("status", "answer_option", "answer_other", "answer_notes", "answered_by_id", "answered_at").
		Update(q); err != nil {
		return err
	}
	if err := addUsersToTaskQuestions(s, []*TaskQuestion{q}); err != nil {
		return err
	}

	events.DispatchOnCommit(s, &TaskQuestionAnsweredEvent{
		Task:     &task,
		Question: q,
		Doer:     doer,
	})
	return nil
}

func addOpenQuestionCountToTasks(s *xorm.Session, taskIDs []int64, taskMap map[int64]*Task) error {
	if len(taskIDs) == 0 {
		return nil
	}

	counts := []struct {
		TaskID int64 `xorm:"task_id"`
		Count  int64 `xorm:"count"`
	}{}
	if err := s.
		Select("task_id, COUNT(*) as count").
		Table("task_questions").
		Where(builder.In("task_id", taskIDs).And(builder.Eq{"status": TaskQuestionStatusOpen})).
		GroupBy("task_id").
		Find(&counts); err != nil {
		return err
	}
	for _, c := range counts {
		if task, ok := taskMap[c.TaskID]; ok {
			task.OpenQuestionCount = c.Count
		}
	}
	return nil
}
