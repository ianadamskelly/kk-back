package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

// --- Admin: course tasks ---

func (a *API) listAdminCourseTasks(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	items, err := a.store.ListCourseTasks(r.Context(), courseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) createCourseTask(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	if _, err := a.store.GetCourseByID(r.Context(), courseID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "course not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in struct {
		Module       string `json:"module"`
		Prompt       string `json:"prompt"`
		RequiredPass bool   `json:"requiredPass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Module == "" || in.Prompt == "" {
		writeError(w, http.StatusBadRequest, "module and prompt are required")
		return
	}
	t := &store.CourseTask{
		CourseID: courseID, Module: in.Module, Prompt: in.Prompt, RequiredPass: in.RequiredPass,
	}
	if err := a.store.AddCourseTask(r.Context(), t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (a *API) updateCourseTask(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	taskID := parseID(chi.URLParam(r, "taskId"))
	var in struct {
		Module       string `json:"module"`
		Prompt       string `json:"prompt"`
		RequiredPass bool   `json:"requiredPass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Module == "" || in.Prompt == "" {
		writeError(w, http.StatusBadRequest, "module and prompt are required")
		return
	}
	t := &store.CourseTask{
		ID: taskID, CourseID: courseID,
		Module: in.Module, Prompt: in.Prompt, RequiredPass: in.RequiredPass,
	}
	if err := a.store.UpdateCourseTask(r.Context(), t); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteCourseTask(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	taskID := parseID(chi.URLParam(r, "taskId"))
	if err := a.store.DeleteCourseTask(r.Context(), courseID, taskID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "task not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Admin: submissions inbox ---

func (a *API) listAdminCourseSubmissions(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	items, err := a.store.AdminListSubmissionsForCourse(r.Context(), courseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	adminID := currentUserID(r)
	for i := range items {
		items[i].FileURL = a.signedFileURL(adminID, items[i].FileURL)
	}
	writeJSON(w, http.StatusOK, items)
}

// listAllAdminSubmissions powers the global grading inbox — every
// student submission across all courses, newest first. The client
// filters by course + grade.
func (a *API) listAllAdminSubmissions(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.AdminListAllSubmissions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	adminID := currentUserID(r)
	for i := range items {
		items[i].FileURL = a.signedFileURL(adminID, items[i].FileURL)
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) gradeSubmission(w http.ResponseWriter, r *http.Request) {
	submissionID := parseID(chi.URLParam(r, "submissionId"))
	graderID := currentUserID(r)
	var in struct {
		Grade    string `json:"grade"`
		Feedback string `json:"feedback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Grade != "passed" && in.Grade != "failed" {
		writeError(w, http.StatusBadRequest, "grade must be 'passed' or 'failed'")
		return
	}
	if err := a.store.GradeSubmission(r.Context(), submissionID, graderID, in.Grade, in.Feedback); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "submission not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// If this was the last required-pass task standing between the
	// student and a certificate, mint + email it now. The helper
	// is a no-op when the course has no required-pass tasks or the
	// student still has outstanding ones.
	if in.Grade == "passed" {
		if userID, courseID, err := a.store.SubmissionContext(r.Context(), submissionID); err == nil && userID > 0 && courseID > 0 {
			a.issueCertificateForCompletion(r.Context(), userID, courseID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Customer: read tasks + submit ---

// listMyCourseTasks returns the tasks for the course bundled with
// the caller's own submissions (keyed by taskId on the client). The
// course must be one the user is enrolled in.
func (a *API) listMyCourseTasks(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	courseSlug := chi.URLParam(r, "slug")
	course, err := a.store.GetCourseBySlug(r.Context(), courseSlug, true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	enrolled, err := a.store.HasUserEnrolledInCourse(r.Context(), uid, course.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !enrolled {
		writeError(w, http.StatusForbidden, "not enrolled in this course")
		return
	}
	tasks, err := a.store.ListCourseTasks(r.Context(), course.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	subs, err := a.store.GetUserSubmissionsForCourse(r.Context(), uid, course.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Replace raw /files/... fileUrls with signed /api/files/<token>
	// URLs so the student's browser can fetch their attachment
	// without an Authorization header.
	for i := range subs {
		subs[i].FileURL = a.signedFileURL(uid, subs[i].FileURL)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks":       tasks,
		"submissions": subs,
	})
}

// submitCourseTask creates or replaces the caller's response. The
// caller must be enrolled in the course owning this task — without
// that gate any signed-in user could post submissions to any task.
func (a *API) submitCourseTask(w http.ResponseWriter, r *http.Request) {
	uid := currentUserID(r)
	taskID := parseID(chi.URLParam(r, "taskId"))

	// Resolve the task's course id so we can verify enrollment. A
	// generic per-task fetch isn't in the store yet — read it
	// through the existing context helper that already joins task
	// -> course via the submissions table. Since the submission
	// may not exist yet, we instead query the task list for the
	// task's course id directly.
	courseID, err := a.store.CourseIDForTask(r.Context(), taskID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	enrolled, err := a.store.HasUserEnrolledInCourse(r.Context(), uid, courseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !enrolled {
		writeError(w, http.StatusForbidden, "not enrolled in this course")
		return
	}

	var in struct {
		Body    string `json:"body"`
		FileURL string `json:"fileUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Body == "" && in.FileURL == "" {
		writeError(w, http.StatusBadRequest, "a written response or an uploaded file is required")
		return
	}
	sub := &store.CourseTaskSubmission{
		TaskID: taskID, UserID: uid, Body: in.Body, FileURL: in.FileURL,
	}
	if err := a.store.UpsertSubmission(r.Context(), sub); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sub)
}
