// Package window открывает нативное GTK-окно с WebKit2GTK и показывает в нём
// URL. Это ~50 строк CGO вместо зависимости webview_go: она требует
// webkit2gtk-4.0, которого в Ubuntu 26.04 больше нет (только 4.1).
//
// Почему без C→Go колбэков: весь UI живёт на встроенном HTTP-сервере и ходит
// по относительным URL — окну достаточно просто загрузить страницу.
package window

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

static void on_destroy(GtkWidget *w, gpointer data) {
	(void)w; (void)data;
	gtk_main_quit();
}

static int cc_open(const char *url, const char *title, const char *icon,
                   int width, int height) {
	if (!gtk_init_check(NULL, NULL)) return -1;

	GtkWidget *win = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_title(GTK_WINDOW(win), title);
	gtk_window_set_default_size(GTK_WINDOW(win), width, height);
	if (icon != NULL) gtk_window_set_icon_name(GTK_WINDOW(win), icon);
	g_signal_connect(win, "destroy", G_CALLBACK(on_destroy), NULL);

	WebKitWebView *vw = WEBKIT_WEB_VIEW(webkit_web_view_new());
	// DevTools выключены по умолчанию: приложение, а не вкладка браузера.
	gtk_container_add(GTK_CONTAINER(win), GTK_WIDGET(vw));
	webkit_web_view_load_uri(vw, url);

	gtk_widget_show_all(win);
	gtk_main(); // блокируется до закрытия окна
	return 0;
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

// Run открывает окно (заголовок title, иконка по имени icon, если задана) и
// блокируется, пока пользователь не закроет окно.
func Run(url, title, icon string, width, height int) error {
	// GTK требует, чтобы всё жило на одном (главном) потоке OS.
	runtime.LockOSThread()

	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))

	var cIcon *C.char
	if icon != "" {
		cIcon = C.CString(icon)
		defer C.free(unsafe.Pointer(cIcon))
	}

	rc := C.cc_open(cURL, cTitle, cIcon, C.int(width), C.int(height))
	if rc != 0 {
		return fmt.Errorf("не удалось открыть окно GTK (нет доступа к дисплею?)")
	}
	return nil
}
