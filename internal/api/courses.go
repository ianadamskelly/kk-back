package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"kuzakizazi/internal/store"
)

type courseInput struct {
	Title         string `json:"title"`
	Slug          string `json:"slug"`
	Summary       string `json:"summary"`
	Description   string `json:"description"`
	CoverImage    string `json:"coverImage"`
	Level         string `json:"level"`
	Duration      string `json:"duration"`
	Instructor    string `json:"instructor"`
	Category      string `json:"category"`
	Language      string `json:"language"`
	PromoVideo    string `json:"promoVideo"`
	Prerequisites string `json:"prerequisites"`
	Outcomes      string `json:"outcomes"`
	PriceCents    int64  `json:"priceCents"`
	Status        string `json:"status"`
	SortOrder     int    `json:"sortOrder"`
}

type lessonInput struct {
	Module    string `json:"module"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	VideoURL  string `json:"videoUrl"`
	Duration  string `json:"duration"`
	IsPreview bool   `json:"isPreview"`
	SortOrder int    `json:"sortOrder"`
}

func (a *API) listPublicCourses(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListCourses(r.Context(), true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getPublicCourse(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetCourseBySlug(r.Context(), chi.URLParam(r, "slug"), true)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	entitled, locked := a.courseEntitlement(r, item)
	// Lock lesson bodies + video URLs for non-entitled viewers on paid courses
	// so a curious client can't just hit /api/courses/{slug} to bypass the
	// paywall. Titles + duration stay visible to advertise the curriculum.
	if locked && !entitled {
		for i := range item.Resources {
			item.Resources[i].URL = ""
		}
		for i := range item.Lessons {
			item.Lessons[i].Content = ""
			item.Lessons[i].VideoURL = ""
			for j := range item.Lessons[i].Resources {
				item.Lessons[i].Resources[j].URL = ""
			}
		}
	} else {
		uid := int64(0)
		if claims := a.optionalClaims(r); claims != nil {
			uid = parseClaimsUserID(claims)
		}
		for i := range item.Resources {
			item.Resources[i].URL = a.signedFileURL(uid, item.Resources[i].URL)
		}
		for i := range item.Lessons {
			for j := range item.Lessons[i].Resources {
				item.Lessons[i].Resources[j].URL = a.signedFileURL(uid, item.Lessons[i].Resources[j].URL)
			}
		}
	}
	writeJSON(w, http.StatusOK, struct {
		*store.Course
		Entitled bool `json:"entitled"`
		Locked   bool `json:"locked"`
	}{Course: item, Entitled: entitled || !locked, Locked: locked})
}

// courseEntitlement returns (entitled, locked) for the requester:
//   - locked=true means the course is paid and access must be checked
//   - entitled=true means the requester has access (member, owner, or admin)
func (a *API) courseEntitlement(r *http.Request, c *store.Course) (entitled, locked bool) {
	locked = c.PriceCents > 0
	if !locked {
		return true, false
	}
	claims := a.optionalClaims(r)
	if claims == nil {
		return false, true
	}
	uid := parseClaimsUserID(claims)
	if uid == 0 {
		return false, true
	}
	if claims.Role == "admin" {
		return true, true
	}
	if active, _ := a.store.IsActiveCourseMember(r.Context(), uid); active {
		return true, true
	}
	if owned, _ := a.store.UserOwnsCourse(r.Context(), uid, c.ID); owned {
		return true, true
	}
	return false, true
}

func (a *API) listAdminCourses(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.ListCourses(r.Context(), false)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) getAdminCourse(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.GetCourseByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) createCourse(w http.ResponseWriter, r *http.Request) {
	var in courseInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	item := &store.Course{
		Title: in.Title, Slug: in.Slug, Summary: in.Summary,
		Description: sanitizeHTML(in.Description),
		CoverImage:  in.CoverImage, Level: in.Level, Duration: in.Duration,
		Instructor: in.Instructor, Category: in.Category, Language: in.Language,
		PromoVideo: in.PromoVideo, Prerequisites: in.Prerequisites, Outcomes: in.Outcomes,
		PriceCents: in.PriceCents, Status: in.Status, SortOrder: in.SortOrder,
	}
	if err := a.store.CreateCourse(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) updateCourse(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetCourseByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in courseInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	existing.Title = in.Title
	existing.Slug = in.Slug
	existing.Summary = in.Summary
	existing.Description = sanitizeHTML(in.Description)
	existing.CoverImage = in.CoverImage
	existing.Level = in.Level
	existing.Duration = in.Duration
	existing.Instructor = in.Instructor
	existing.Category = in.Category
	existing.Language = in.Language
	existing.PromoVideo = in.PromoVideo
	existing.Prerequisites = in.Prerequisites
	existing.Outcomes = in.Outcomes
	existing.PriceCents = in.PriceCents
	existing.Status = in.Status
	existing.SortOrder = in.SortOrder
	if err := a.store.UpdateCourse(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

func (a *API) deleteCourse(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteCourse(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Lessons ---

func (a *API) listCourseLessons(w http.ResponseWriter, r *http.Request) {
	lessons, err := a.store.ListLessons(r.Context(), parseID(chi.URLParam(r, "id")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lessons)
}

func (a *API) createLesson(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	if _, err := a.store.GetCourseByID(r.Context(), courseID); err != nil {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	var in lessonInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	lesson := &store.Lesson{
		CourseID: courseID, Module: in.Module, Slug: in.Slug, Title: in.Title,
		Content: sanitizeHTML(in.Content), VideoURL: in.VideoURL, Duration: in.Duration,
		IsPreview: in.IsPreview, SortOrder: in.SortOrder,
	}
	if err := a.store.CreateLesson(r.Context(), lesson); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, lesson)
}

func (a *API) getLesson(w http.ResponseWriter, r *http.Request) {
	lesson, err := a.store.GetLessonByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "lesson not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lesson)
}

func (a *API) updateLesson(w http.ResponseWriter, r *http.Request) {
	existing, err := a.store.GetLessonByID(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "lesson not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var in lessonInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if in.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	existing.Module = in.Module
	existing.Slug = in.Slug
	existing.Title = in.Title
	existing.Content = sanitizeHTML(in.Content)
	existing.VideoURL = in.VideoURL
	existing.Duration = in.Duration
	existing.IsPreview = in.IsPreview
	existing.SortOrder = in.SortOrder
	if err := a.store.UpdateLesson(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

// reorderLessons accepts the new ordering for every lesson in a course
// and writes it in one transaction. Lessons may also change module.
func (a *API) reorderLessons(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	if _, err := a.store.GetCourseByID(r.Context(), courseID); err != nil {
		writeError(w, http.StatusNotFound, "course not found")
		return
	}
	var in struct {
		Items []struct {
			ID        int64  `json:"id"`
			Module    string `json:"module"`
			SortOrder int    `json:"sortOrder"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	items := make([]store.Lesson, len(in.Items))
	for i, it := range in.Items {
		items[i] = store.Lesson{ID: it.ID, Module: it.Module, SortOrder: it.SortOrder}
	}
	if err := a.store.ReorderLessons(r.Context(), courseID, items); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteLesson(w http.ResponseWriter, r *http.Request) {
	err := a.store.DeleteLesson(r.Context(), parseID(chi.URLParam(r, "id")))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "lesson not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Course resources (links/files attached to a course or lesson) ---

func (a *API) listCourseResources(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	items, err := a.store.ListCourseResources(r.Context(), courseID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (a *API) addCourseResource(w http.ResponseWriter, r *http.Request) {
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
		LessonID *int64 `json:"lessonId"`
		Label    string `json:"label"`
		URL      string `json:"url"`
		Kind     string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.URL == "" || in.Label == "" {
		writeError(w, http.StatusBadRequest, "label and url are required")
		return
	}
	res := &store.CourseResource{
		CourseID: courseID,
		LessonID: in.LessonID,
		Label:    in.Label,
		URL:      in.URL,
		Kind:     in.Kind,
	}
	if err := a.store.AddCourseResource(r.Context(), res); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (a *API) deleteCourseResource(w http.ResponseWriter, r *http.Request) {
	courseID := parseID(chi.URLParam(r, "id"))
	resourceID := parseID(chi.URLParam(r, "resourceId"))
	if err := a.store.DeleteCourseResource(r.Context(), courseID, resourceID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "resource not found for this course")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
