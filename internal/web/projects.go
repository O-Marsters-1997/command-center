package web

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/O-Marsters-1997/command-center/internal/store"
)

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errorf(http.StatusBadRequest, "malformed form: %w", err)
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
	project := store.NewProject{Name: name, Hue: hue, Repos: repos, At: s.clock.Now()}
	if err := s.store.CreateProject(r.Context(), project); err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	return s.respondRepoGroups(w, r)
}

func (s *Server) handleEditProject(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errorf(http.StatusBadRequest, "malformed form: %w", err)
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
		return fmt.Errorf("edit project: %w", err)
	}
	return s.respondRepoGroups(w, r)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.DeleteProject(r.Context(), r.PathValue("name")); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return s.respondRepoGroups(w, r)
}

func (s *Server) handleSetRepoProject(w http.ResponseWriter, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return errorf(http.StatusBadRequest, "malformed form: %w", err)
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("name")
	if !repoName.MatchString(repo) || hasDotSegment(repo) {
		return errorf(http.StatusBadRequest, "repo %q is not owner/name", repo)
	}
	if err := s.store.SetRepoProject(r.Context(), repo, strings.TrimSpace(r.PostForm.Get("project"))); err != nil {
		return fmt.Errorf("set repo project: %w", err)
	}
	return s.respondRepoGroups(w, r)
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
		return fmt.Errorf("render repo groups: %w", err)
	}
	return renderHTML(w, "repoGroups", groups)
}
