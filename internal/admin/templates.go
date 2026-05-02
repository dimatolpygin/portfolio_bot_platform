package admin

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"os"
)

//go:embed templates/*.html
var templateFS embed.FS

// devTemplatesDir — папка шаблонов относительно рабочей директории.
// Если она существует на диске, шаблоны перечитываются при каждом запросе
// (dev-режим: изменения видны без пересборки). Иначе используется embed.FS.
const devTemplatesDir = "internal/admin/templates"

type templateRenderer struct {
	templates *template.Template // nil → live-загрузка с диска
	funcs     template.FuncMap
	logger    *slog.Logger
}

func newTemplateRenderer(logger *slog.Logger) (*templateRenderer, error) {
	funcs := template.FuncMap{
		"svg_home":      svgHome,
		"svg_users":     svgUsers,
		"svg_briefcase": svgBriefcase,
		"svg_logout":    svgLogout,
		"svg_person":    svgPerson,
		"svg_bell":      svgBell,
	}

	tr := &templateRenderer{funcs: funcs, logger: logger}

	if _, err := os.Stat(devTemplatesDir); err == nil {
		// Dev: шаблоны есть на диске — делаем первый парс для проверки ошибок
		if _, err := tr.parseFromDisk(); err != nil {
			return nil, err
		}
		logger.Info("admin templates: live reload from disk", "dir", devTemplatesDir)
		return tr, nil
	}

	// Production: используем вшитые шаблоны
	tmpl, err := template.New("").Funcs(funcs).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	tr.templates = tmpl
	return tr, nil
}

func (tr *templateRenderer) parseFromDisk() (*template.Template, error) {
	return template.New("").Funcs(tr.funcs).ParseGlob(devTemplatesDir + "/*.html")
}

func (tr *templateRenderer) Render(w http.ResponseWriter, name string, data any) {
	tmpl := tr.templates
	if tmpl == nil {
		var err error
		tmpl, err = tr.parseFromDisk()
		if err != nil {
			tr.logger.Error("template parse", "error", err)
			http.Error(w, "Ошибка шаблона: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "Ошибка шаблона: "+err.Error(), http.StatusInternalServerError)
	}
}

func svgHome() template.HTML {
	return `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.8" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="m2.25 12 8.954-8.955a1.126 1.126 0 0 1 1.591 0L21.75 12M4.5 9.75v10.125c0 .621.504 1.125 1.125 1.125H9.75v-4.875c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125V21h4.125c.621 0 1.125-.504 1.125-1.125V9.75M8.25 21h8.25"/></svg>`
}

func svgUsers() template.HTML {
	return `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.8" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M15 19.128a9.38 9.38 0 0 0 2.625.372 9.337 9.337 0 0 0 4.121-.952 4.125 4.125 0 0 0-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 0 1 8.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0 1 11.964-3.07M12 6.375a3.375 3.375 0 1 1-6.75 0 3.375 3.375 0 0 1 6.75 0Zm8.25 2.25a2.625 2.625 0 1 1-5.25 0 2.625 2.625 0 0 1 5.25 0Z"/></svg>`
}

func svgBriefcase() template.HTML {
	return `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.8" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M20.25 14.15v4.25c0 1.094-.787 2.036-1.872 2.18-2.087.277-4.216.42-6.378.42s-4.291-.143-6.378-.42c-1.085-.144-1.872-1.086-1.872-2.18v-4.25m16.5 0a2.18 2.18 0 0 0 .75-1.661V8.706c0-1.081-.768-2.015-1.837-2.175a48.114 48.114 0 0 0-3.413-.387m4.5 8.006c-.194.165-.42.295-.673.38A23.978 23.978 0 0 1 12 15.75c-2.648 0-5.195-.429-7.577-1.22a2.016 2.016 0 0 1-.673-.38m0 0A2.18 2.18 0 0 1 3 12.489V8.706c0-1.081.768-2.015 1.837-2.175a48.111 48.111 0 0 1 3.413-.387m7.5 0V5.25A2.25 2.25 0 0 0 13.5 3h-3a2.25 2.25 0 0 0-2.25 2.25v.894m7.5 0a48.667 48.667 0 0 0-7.5 0"/></svg>`
}

func svgLogout() template.HTML {
	return `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.8" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M8.25 9V5.25A2.25 2.25 0 0 1 10.5 3h6a2.25 2.25 0 0 1 2.25 2.25v13.5A2.25 2.25 0 0 1 16.5 21h-6a2.25 2.25 0 0 1-2.25-2.25V15m-3 0-3-3m0 0 3-3m-3 3H15"/></svg>`
}

func svgBell() template.HTML {
	return `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.8" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M14.857 17.082a23.848 23.848 0 0 0 5.454-1.31A8.967 8.967 0 0 1 18 9.75V9A6 6 0 0 0 6 9v.75a8.967 8.967 0 0 1-2.312 6.022c1.733.64 3.56 1.085 5.455 1.31m5.714 0a24.255 24.255 0 0 1-5.714 0m5.714 0a3 3 0 1 1-5.714 0"/></svg>`
}

func svgPerson() template.HTML {
	return `<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.8" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" d="M17.982 18.725A7.488 7.488 0 0 0 12 15.75a7.488 7.488 0 0 0-5.982 2.975m11.963 0a9 9 0 1 0-11.963 0m11.963 0A8.966 8.966 0 0 1 12 21a8.966 8.966 0 0 1-5.982-2.275M15 9.75a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z"/></svg>`
}

