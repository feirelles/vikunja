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

package apiv2

import (
	"context"
	"fmt"
	"net/http"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web/handler"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/conditional"
)

type taskQuestionListBody struct {
	Body Paginated[*models.TaskQuestion]
}

// RegisterTaskQuestionRoutes wires the questions asked on a task onto the Huma API.
func RegisterTaskQuestionRoutes(api huma.API) {
	tags := []string{"task_questions"}

	Register(api, huma.Operation{
		OperationID: "task-questions-list",
		Summary:     "List the questions of a task",
		Description: "Returns the questions asked on the given task, open ones first, then oldest first. Requires read access to the task. Pass status to only get questions in that state.",
		Method:      http.MethodGet,
		Path:        "/tasks/{task}/questions",
		Tags:        tags,
	}, taskQuestionsList)

	Register(api, huma.Operation{
		OperationID: "task-questions-read",
		Summary:     "Get a question of a task",
		Description: "Returns one question with its answer, if any. The question must belong to the task in the path. Sends an ETag; pass it as If-None-Match on a later read to get a 304 Not Modified.",
		Method:      http.MethodGet,
		Path:        "/tasks/{task}/questions/{question}",
		Tags:        tags,
	}, taskQuestionsRead)

	Register(api, huma.Operation{
		OperationID: "task-questions-create",
		Summary:     "Ask a question on a task",
		Description: "Adds an open question to the task, with optional options (unique keys such as A, B, C) and a recommended option. The author is the authenticated user. Requires write access to the task. Emits task.question.created, which webhooks can subscribe to.",
		Method:      http.MethodPost,
		Path:        "/tasks/{task}/questions",
		Tags:        tags,
	}, taskQuestionsCreate)

	Register(api, huma.Operation{
		OperationID: "task-questions-update",
		Summary:     "Update a question of a task",
		Description: "Replaces the question's title, summary, details, options and recommendation. status may be set to cancelled, or to open to reopen an answered question (which clears its answer); answering goes through the answer endpoint. Requires write access to the task. Use PATCH for a partial update.",
		Method:      http.MethodPut,
		Path:        "/tasks/{task}/questions/{question}",
		Tags:        tags,
	}, taskQuestionsUpdate)

	Register(api, huma.Operation{
		OperationID: "task-questions-delete",
		Summary:     "Delete a question of a task",
		Description: "Deletes the question and its answer. Requires write access to the task.",
		Method:      http.MethodDelete,
		Path:        "/tasks/{task}/questions/{question}",
		Tags:        tags,
	}, taskQuestionsDelete)

	Register(api, huma.Operation{
		OperationID: "task-questions-answer",
		Summary:     "Answer a question of a task",
		Description: "Answers an open or deferred question with exactly one of option (a key of its options), other (free text) or defer. notes may accompany any of them. An answered or cancelled question is rejected with 412; reopen it first. Requires write access to the task. Emits task.question.answered with the answer, which webhooks can subscribe to.",
		Method:      http.MethodPost,
		Path:        "/tasks/{task}/questions/{question}/answer",
		Tags:        tags,
		// Answering changes an existing question; nothing is created.
		DefaultStatus: http.StatusOK,
	}, taskQuestionsAnswer)
}

func init() { AddRouteRegistrar(RegisterTaskQuestionRoutes) }

func taskQuestionsList(ctx context.Context, in *struct {
	TaskID int64  `path:"task"`
	Status string `query:"status" enum:"open,answered,deferred,cancelled" doc:"Only return questions in this state."`
	ListParams
}) (*taskQuestionListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	filter := &models.TaskQuestion{TaskID: in.TaskID, Status: models.TaskQuestionStatus(in.Status)}
	result, _, total, err := handler.DoReadAll(ctx, filter, a, in.Q, in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.TaskQuestion)
	if !ok {
		return nil, fmt.Errorf("taskQuestions.ReadAll returned unexpected type %T (expected []*models.TaskQuestion)", result)
	}
	return &taskQuestionListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

type taskQuestionReadBody struct {
	models.TaskQuestion
	MaxPermission models.Permission `json:"max_permission" readOnly:"true" doc:"The maximum permission the requesting user has on the question's task (0=read, 1=read/write, 2=admin). Answering needs read/write."`
}

func taskQuestionsRead(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
	ID     int64 `path:"question"`
	conditional.Params
}) (*singleReadBody[taskQuestionReadBody], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	question := &models.TaskQuestion{ID: in.ID, TaskID: in.TaskID}
	maxPermission, err := handler.DoReadOne(ctx, question, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body := &taskQuestionReadBody{TaskQuestion: *question, MaxPermission: models.Permission(maxPermission)}
	return conditionalReadResponse(&in.Params, body, question.Updated, maxPermission)
}

func taskQuestionsCreate(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
	Body   models.TaskQuestion
}) (*singleBody[models.TaskQuestion], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.TaskID = in.TaskID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.TaskQuestion]{Body: &in.Body}, nil
}

// Body matches the read shape so AutoPatch's GET→PUT echo of max_permission validates.
func taskQuestionsUpdate(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
	ID     int64 `path:"question"`
	Body   taskQuestionReadBody
}) (*singleBody[models.TaskQuestion], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	question := &in.Body.TaskQuestion
	question.ID = in.ID         // URL wins over body
	question.TaskID = in.TaskID // parent from the path scopes the update
	if err := handler.DoUpdate(ctx, question, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.TaskQuestion]{Body: question}, nil
}

func taskQuestionsDelete(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
	ID     int64 `path:"question"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.TaskQuestion{ID: in.ID, TaskID: in.TaskID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}

func taskQuestionsAnswer(ctx context.Context, in *struct {
	TaskID int64 `path:"task"`
	ID     int64 `path:"question"`
	Body   models.TaskQuestionAnswer
}) (*singleBody[models.TaskQuestion], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	s := db.NewSession()
	defer s.Close()
	fail := func(err error) (*singleBody[models.TaskQuestion], error) {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, err
	}

	question := &models.TaskQuestion{ID: in.ID, TaskID: in.TaskID}
	can, err := question.CanUpdate(s, a)
	if err != nil {
		return fail(translateDomainError(err))
	}
	if !can {
		return fail(huma.Error403Forbidden("forbidden"))
	}
	if err := question.Answer(s, a, &in.Body); err != nil {
		return fail(translateDomainError(err))
	}
	if err := s.Commit(); err != nil {
		return fail(translateDomainError(err))
	}
	events.DispatchPending(ctx, s)
	return &singleBody[models.TaskQuestion]{Body: question}, nil
}
