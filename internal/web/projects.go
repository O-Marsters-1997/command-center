package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errorf(http.StatusBadRequest, "malformed form: %v", err)
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	if !store.ProjectNamePattern.MatchString(name) {
		return errorf(http.StatusBadRequest, "project name %q is malformed", name)
	}
	hue, err := strconv.Atoi(r.PostForm.Get("hue"))
	if err != nil || hue < store.MinHue || hue > store.MaxHue {
		return errorf(http.StatusBadRequest, "hue %q is not %d to %d", r.PostForm.Get("hue"), store.MinHue, store.MaxHue)
	}
	repos := r.PostForm["repo"]
	for _, repo := range repos {
		if !repoName.MatchString(repo) || hasDotSegment(repo) {
			return errorf(http.StatusBadRequest, "repo %q is not owner/name", repo)
		}
	}
	if err := s.store.CreateProject(r.Context(), name, hue, repos, s.clock.Now()); err != nil {
		return projectError(err)
	}
	return s.respondRepoGroups(w, r)
}

func (s *Server) handleEditProject(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errorf(http.StatusBadRequest, "malformed form: %v", err)
	}
	name := r.PathValue("name")
	newName := strings.TrimSpace(r.PostForm.Get("name"))
	if !store.ProjectNamePattern.MatchString(newName) {
		return errorf(http.StatusBadRequest, "project name %q is malformed", newName)
	}
	hue, err := strconv.Atoi(r.PostForm.Get("hue"))
	if err != nil {
		return errorf(http.StatusBadRequest, "hue %q is not %d to %d", r.PostForm.Get("hue"), store.MinHue, store.MaxHue)
	}
	if err := s.store.EditProject(r.Context(), name, newName, hue); err != nil {
		return projectError(err)
	}
	return s.respondRepoGroups(w, r)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteProject(r.Context(), r.PathValue("name")); err != nil {
		return projectError(err)
	}
	return s.respondRepoGroups(w, r)
}

func (s *Server) handleSetRepoProject(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errorf(http.StatusBadRequest, "malformed form: %v", err)
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("name")
	if !repoName.MatchString(repo) || hasDotSegment(repo) {
		return errorf(http.StatusBadRequest, "repo %q is not owner/name", repo)
	}
	if err := s.store.SetRepoProject(r.Context(), repo, strings.TrimSpace(r.PostForm.Get("project"))); err != nil {
		return projectError(err)
	}
	return s.respondRepoGroups(w, r)
}

func projectError(err error) error {
	switch {
	case errors.Is(err, store.ErrProjectExists):
		return withStatus(http.StatusConflict, err)
	case errors.Is(err, store.ErrNotFound):
		return withStatus(http.StatusNotFound, err)
	case errors.Is(err, store.ErrInvalidProject):
		return withStatus(http.StatusBadRequest, err)
	}
	return err
}

func (s *Server) respondRepoGroups(w http.ResponseWriter, r *http.Request) error {
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/repos", http.StatusSeeOther)
		return nil
	}
	return s.renderRepoGroups(w, r)
}

func (s *Server) renderRepoGroups(w http.ResponseWriter, r *http.Request) error {
	groups, err := s.view.RepoGroups(r.Context())
	if err != nil {
		return err
	}
	return renderHTML(w, "repoGroups", groups)
}
