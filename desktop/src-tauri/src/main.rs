// Homing 桌面壳：托盘菜单 + 本机控制台窗口 + homingd sidecar
//
// 设计原则：壳只做三件事——把控制台装进窗口、把「等你处理 / 运行中」放进托盘、
// 保证 homingd 在跑。业务逻辑一律在 homingd 里，壳不存任何会话数据。

#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use std::collections::HashMap;
use std::io::{Read, Write};
use std::net::TcpStream;
use std::process::{Child, Command, Stdio};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use serde_json::Value;
use tauri::menu::{CheckMenuItem, IsMenuItem, Menu, MenuItem, PredefinedMenuItem};
use tauri::tray::TrayIconBuilder;
use tauri::{AppHandle, Manager, RunEvent, WebviewUrl, WebviewWindowBuilder};
use tauri_plugin_autostart::ManagerExt as AutostartExt;

const PORT: u16 = 18787;
const VERSION: &str = env!("CARGO_PKG_VERSION");

// ---------------------------------------------------------------- 本机 HTTP

/// 只跟 127.0.0.1 上的 homingd 说话：明文、短超时、Connection: close。
fn http(port: u16, method: &str, path: &str, body: Option<&str>) -> Option<String> {
    let addr = format!("127.0.0.1:{port}");
    let socket_addr = addr.parse().ok()?;
    let mut stream = TcpStream::connect_timeout(&socket_addr, Duration::from_millis(600)).ok()?;
    stream.set_read_timeout(Some(Duration::from_millis(2500))).ok()?;
    stream.set_write_timeout(Some(Duration::from_millis(1500))).ok()?;

    let body_str = body.unwrap_or("");
    let request = format!(
        "{method} {path} HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\nAccept: application/json\r\nConnection: close\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{body_str}",
        body_str.as_bytes().len()
    );
    stream.write_all(request.as_bytes()).ok()?;

    let mut raw = Vec::new();
    let mut chunk = [0u8; 8192];
    loop {
        match stream.read(&mut chunk) {
            Ok(0) => break,
            Ok(n) => raw.extend_from_slice(&chunk[..n]),
            Err(_) => break,
        }
        if raw.len() > 2 * 1024 * 1024 {
            break;
        }
    }
    let text = String::from_utf8_lossy(&raw).to_string();
    let split = text.find("\r\n\r\n")?;
    Some(text[split + 4..].to_string())
}

fn get_json(path: &str) -> Option<Value> {
    let body = http(PORT, "GET", path, None)?;
    serde_json::from_str(&body).ok()
}

/// 起服务的一瞬间本机偶尔会连不上（relay 刚启动、或者它正在扫会话），
/// 所以探活多试两次，别因为一次失败就去拉第二个 homingd。
fn healthy() -> bool {
    for attempt in 0..3 {
        if http(PORT, "GET", "/health", None).is_some() {
            return true;
        }
        if attempt < 2 {
            std::thread::sleep(Duration::from_millis(350));
        }
    }
    false
}

// ---------------------------------------------------------------- 状态

#[derive(Default, Clone)]
struct Snapshot {
    online: bool,
    waiting: usize,
    running: usize,
    paused_minutes: u32,
    waiting_items: Vec<(String, String)>,
    running_items: Vec<(String, String)>,
}

#[derive(Clone)]
struct ShellState {
    child: Arc<Mutex<Option<Child>>>,
    snapshot: Arc<Mutex<Snapshot>>,
    autostart: Arc<Mutex<bool>>,
}

fn cut(text: &str, max: usize) -> String {
    let clean = text.split_whitespace().collect::<Vec<_>>().join(" ");
    if clean.chars().count() <= max {
        clean
    } else {
        let mut out: String = clean.chars().take(max.saturating_sub(1)).collect();
        out.push('…');
        out
    }
}

fn agent_label(source: &str) -> &str {
    match source {
        "codex" => "Codex",
        "claude" => "Claude Code",
        "workbuddy" => "WorkBuddy",
        "opencode" => "OpenCode",
        "zcode" => "ZCode",
        other => other,
    }
}

fn project_of(item: &Value) -> String {
    let session = item.get("session").cloned().unwrap_or(Value::Null);
    if let Some(project) = session.get("project").and_then(Value::as_str) {
        if !project.is_empty() {
            return project.to_string();
        }
    }
    if let Some(cwd) = session.get("cwd").and_then(Value::as_str) {
        if let Some(last) = cwd.rsplit(['/', '\\']).find(|part| !part.is_empty()) {
            return last.to_string();
        }
    }
    session
        .get("title")
        .and_then(Value::as_str)
        .unwrap_or("会话")
        .to_string()
}

fn session_id_of(item: &Value) -> String {
    item.get("session")
        .and_then(|s| s.get("session_id"))
        .and_then(Value::as_str)
        .unwrap_or("")
        .to_string()
}

fn format_elapsed(elapsed: Duration) -> String {
    let total = elapsed.as_secs();
    let minutes = total / 60;
    let seconds = total % 60;
    if minutes >= 60 {
        format!("{}:{:02}:{:02}", minutes / 60, minutes % 60, seconds)
    } else {
        format!("{minutes:02}:{seconds:02}")
    }
}

/// 每 8 秒抓一次收件箱与暂停状态，变成托盘能直接用的字符串
fn poll_once(state: &ShellState, started: &mut HashMap<String, Instant>) {
    let online = healthy();
    let mut snapshot = Snapshot {
        online,
        ..Default::default()
    };

    if online {
        if let Some(inbox) = get_json("/api/inbox") {
            if let Some(waiting) = inbox.get("waiting").and_then(Value::as_array) {
                for item in waiting {
                    let source = item
                        .get("session")
                        .and_then(|s| s.get("source"))
                        .and_then(Value::as_str)
                        .unwrap_or("agent");
                    let title = item
                        .get("title")
                        .and_then(Value::as_str)
                        .filter(|t| !t.is_empty())
                        .unwrap_or("需要处理");
                    let label = format!(
                        "{} · {} · {}",
                        agent_label(source),
                        project_of(item),
                        cut(title, 24)
                    );
                    snapshot
                        .waiting_items
                        .push((session_id_of(item), cut(&label, 56)));
                }
                snapshot.waiting = snapshot.waiting_items.len();
            }
            if let Some(running) = inbox.get("running").and_then(Value::as_array) {
                for item in running {
                    let source = item
                        .get("session")
                        .and_then(|s| s.get("source"))
                        .and_then(Value::as_str)
                        .unwrap_or("agent");
                    let id = session_id_of(item);
                    let first_seen = *started.entry(id.clone()).or_insert_with(Instant::now);
                    let label = format!(
                        "{} · {} · {}",
                        agent_label(source),
                        project_of(item),
                        format_elapsed(first_seen.elapsed())
                    );
                    snapshot.running_items.push((id, cut(&label, 56)));
                }
                snapshot.running = snapshot.running_items.len();
            }
        }
        if let Some(pause) = get_json("/api/push/pause") {
            if pause.get("paused").and_then(Value::as_bool).unwrap_or(false) {
                snapshot.paused_minutes = pause.get("minutes").and_then(Value::as_u64).unwrap_or(0) as u32;
            }
        }
    }

    // 结束的会话从计时表里清掉，避免越攒越多
    let live: Vec<String> = snapshot.running_items.iter().map(|(id, _)| id.clone()).collect();
    started.retain(|id, _| live.contains(id));

    if let Ok(mut guard) = state.snapshot.lock() {
        *guard = snapshot;
    }
}

// ---------------------------------------------------------------- 菜单

fn build_menu(app: &AppHandle, snap: &Snapshot, autostart_on: bool) -> tauri::Result<Menu<tauri::Wry>> {
    let status_text = if !snap.online {
        "Homing · 本机服务未响应".to_string()
    } else if snap.paused_minutes > 0 {
        format!("Homing · 已连接 · 推送暂停 {} 分钟", snap.paused_minutes)
    } else {
        "Homing · 已连接".to_string()
    };
    let status = MenuItem::with_id(app, "status", &status_text, false, None::<&str>)?;

    let mut items: Vec<MenuItem<tauri::Wry>> = Vec::new();
    let mut headers: Vec<MenuItem<tauri::Wry>> = Vec::new();

    if !snap.waiting_items.is_empty() {
        headers.push(MenuItem::with_id(
            app,
            "h_waiting",
            format!("等你处理 · {}", snap.waiting),
            false,
            None::<&str>,
        )?);
        for (id, label) in &snap.waiting_items {
            items.push(MenuItem::with_id(
                app,
                format!("wait:{id}"),
                label,
                true,
                None::<&str>,
            )?);
        }
    }

    if !snap.running_items.is_empty() {
        headers.push(MenuItem::with_id(
            app,
            "h_running",
            format!("运行中 · {}", snap.running),
            false,
            None::<&str>,
        )?);
        for (id, label) in &snap.running_items {
            items.push(MenuItem::with_id(
                app,
                format!("run:{id}"),
                label,
                true,
                None::<&str>,
            )?);
        }
    }

    let sep1 = PredefinedMenuItem::separator(app)?;
    let open = MenuItem::with_id(app, "open", "打开控制台", true, Some("CmdOrCtrl+O"))?;
    let pair = MenuItem::with_id(app, "pair", "配对新手机…", true, None::<&str>)?;
    let pause = MenuItem::with_id(
        app,
        "pause",
        if snap.paused_minutes > 0 {
            "恢复推送".to_string()
        } else {
            "暂停推送 1 小时".to_string()
        },
        true,
        None::<&str>,
    )?;
    let autostart = CheckMenuItem::with_id(app, "autostart", "开机自动启动", true, autostart_on, None::<&str>)?;
    let update = MenuItem::with_id(app, "update", "检查更新", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "退出 Homing", true, Some("CmdOrCtrl+Q"))?;
    let sep2 = PredefinedMenuItem::separator(app)?;
    let footer = MenuItem::with_id(
        app,
        "footer",
        format!("版本 {VERSION} · 本机服务 127.0.0.1:{PORT}"),
        false,
        None::<&str>,
    )?;

    let mut refs: Vec<&dyn IsMenuItem<tauri::Wry>> = Vec::new();
    refs.push(&status);
    if !headers.is_empty() || !items.is_empty() {
        refs.push(&sep1);
    }
    for header in &headers {
        refs.push(header);
    }
    for item in &items {
        refs.push(item);
    }
    refs.push(&sep1);
    refs.push(&open);
    refs.push(&pair);
    refs.push(&pause);
    refs.push(&autostart);
    refs.push(&update);
    refs.push(&quit);
    refs.push(&sep2);
    refs.push(&footer);

    Menu::with_items(app, &refs)
}

fn refresh_tray(app: &AppHandle) {
    let state = app.state::<ShellState>();
    let snapshot = state.snapshot.lock().map(|s| s.clone()).unwrap_or_default();
    let autostart_on = *state.autostart.lock().unwrap_or_else(|e| e.into_inner());
    if let Ok(menu) = build_menu(app, &snapshot, autostart_on) {
        if let Some(tray) = app.tray_by_id("main") {
            let _ = tray.set_menu(Some(menu));
        }
    }
    refresh_title(app, &snapshot);
}

/// macOS 菜单栏图标旁边的小字提示（Windows 上只是 tooltip）
fn refresh_title(app: &AppHandle, snap: &Snapshot) {
    if let Some(tray) = app.tray_by_id("main") {
        let tooltip = if !snap.online {
            "Homing · 本机服务未响应".to_string()
        } else if snap.waiting > 0 {
            format!("Homing · {} 件事等你处理", snap.waiting)
        } else if snap.running > 0 {
            format!("Homing · {} 个在运行", snap.running)
        } else {
            "Homing · 已连接".to_string()
        };
        let _ = tray.set_tooltip(Some(tooltip));
    }
}

// ---------------------------------------------------------------- 窗口

fn show_console(app: &AppHandle, path: &str) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
        if path != "/console" {
            let _ = window.eval(format!("window.location.href = 'http://127.0.0.1:{PORT}{path}'"));
        }
        return;
    }
    let url = format!("http://127.0.0.1:{PORT}{path}");
    match tauri::Url::parse(&url) {
        Ok(parsed) => {
            let builder = WebviewWindowBuilder::new(app, "main", WebviewUrl::External(parsed))
                .title("Homing")
                .inner_size(1180.0, 820.0)
                .min_inner_size(880.0, 600.0);
            match builder.build() {
                Ok(_) => println!("[homing] 控制台窗口已创建"),
                Err(err) => eprintln!("[homing] 打开窗口失败: {err}"),
            }
        }
        Err(err) => eprintln!("[homing] 控制台地址解析失败: {err}"),
    }
}

// ---------------------------------------------------------------- sidecar

fn sidecar_path(app: &AppHandle) -> Option<std::path::PathBuf> {
    let name = if cfg!(windows) { "homingd.exe" } else { "homingd" };
    let mut candidates = Vec::new();
    if let Ok(dir) = app.path().resource_dir() {
        candidates.push(dir.join("binaries").join(name));
        candidates.push(dir.join(name));
    }
    if let Ok(exe) = std::env::current_exe() {
        if let Some(dir) = exe.parent() {
            candidates.push(dir.join(name));
            candidates.push(dir.join("binaries").join(name));
            // Contents/MacOS → Contents/Resources/binaries：直接跑二进制（没走 LaunchServices）时兜底
            candidates.push(dir.join("../Resources/binaries").join(name));
            candidates.push(dir.join("../Resources").join(name));
        }
    }
    candidates.into_iter().find(|p| p.exists())
}

fn ensure_homingd(app: &AppHandle) {
    if healthy() {
        println!("[homing] 18787 上已经有 homingd 在跑，直接复用");
        return;
    }
    let Some(bin) = sidecar_path(app) else {
        eprintln!("[homing] 没找到 homingd sidecar，控制台可能打不开");
        return;
    };
    let mut command = Command::new(&bin);
    command
        .env("PORT", PORT.to_string())
        .env("HOMING_DESKTOP", "1")
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null());
    #[cfg(target_os = "windows")]
    {
        use std::os::windows::process::CommandExt;
        command.creation_flags(0x08000000); // CREATE_NO_WINDOW
    }
    match command.spawn() {
        Ok(child) => {
            println!("[homing] 已拉起 sidecar: {}", bin.display());
            if let Ok(mut guard) = app.state::<ShellState>().child.lock() {
                *guard = Some(child);
            }
        }
        Err(err) => eprintln!("[homing] sidecar 启动失败: {err}"),
    }
}

fn stop_homingd(app: &AppHandle) {
    let state = app.state::<ShellState>();
    let mut guard = match state.child.lock() {
        Ok(guard) => guard,
        Err(poisoned) => poisoned.into_inner(),
    };
    if let Some(mut child) = guard.take() {
        let _ = child.kill();
        let _ = child.wait();
    }
}

fn wait_for_health(max: Duration) -> bool {
    let start = Instant::now();
    while start.elapsed() < max {
        if healthy() {
            return true;
        }
        std::thread::sleep(Duration::from_millis(220));
    }
    false
}

// ---------------------------------------------------------------- 主函数

fn main() {
    let state = ShellState {
        child: Arc::new(Mutex::new(None)),
        snapshot: Arc::new(Mutex::new(Snapshot::default())),
        autostart: Arc::new(Mutex::new(false)),
    };

    let app = tauri::Builder::default()
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            None,
        ))
        .manage(state)
        .setup(|app| {
            let handle = app.handle().clone();

            // 1) 开机自启状态（插件管的是 App 自己，不是 homingd）
            if let Ok(enabled) = handle.autolaunch().is_enabled() {
                if let Ok(mut guard) = app.state::<ShellState>().autostart.lock() {
                    *guard = enabled;
                }
            }

            // 2) 没有 homingd 就拉一个，等它起来再做窗口和菜单
            ensure_homingd(&handle);
            let up = wait_for_health(Duration::from_secs(12));

            // 3) 托盘：灰鸽模板图标 + 菜单
            let snapshot = Snapshot {
                online: up,
                ..Default::default()
            };
            let autostart_on = *app.state::<ShellState>().autostart.lock().unwrap_or_else(|e| e.into_inner());
            let menu = build_menu(&handle, &snapshot, autostart_on)?;
            let icon = tauri::image::Image::from_bytes(include_bytes!("../icons/tray-template.png"))?;
            TrayIconBuilder::with_id("main")
                .icon(icon)
                .icon_as_template(true)
                .menu(&menu)
                .show_menu_on_left_click(true)
                .tooltip("Homing")
                .on_tray_icon_event(|tray, event| {
                    // 左键点图标直接开控制台（右键/菜单按钮出菜单）
                    if let tauri::tray::TrayIconEvent::Click {
                        button: tauri::tray::MouseButton::Left,
                        button_state: tauri::tray::MouseButtonState::Up,
                        ..
                    } = event
                    {
                        let app = tray.app_handle().clone();
                        show_console(&app, "/console");
                    }
                })
                .build(app)?;
            println!("[homing] 托盘已就绪（工具提示与菜单每 8 秒刷新一次）");

            // 4) 后台轮询：更新托盘菜单
            let poll_state = ShellState {
                child: app.state::<ShellState>().child.clone(),
                snapshot: app.state::<ShellState>().snapshot.clone(),
                autostart: app.state::<ShellState>().autostart.clone(),
            };
            let poll_handle = handle.clone();
            std::thread::spawn(move || {
                let mut started: HashMap<String, Instant> = HashMap::new();
                loop {
                    poll_once(&poll_state, &mut started);
                    let app_for_main = poll_handle.clone();
                    let _ = poll_handle.run_on_main_thread(move || refresh_tray(&app_for_main));
                    std::thread::sleep(Duration::from_secs(8));
                }
            });

            // 5) 菜单事件
            let menu_handle = handle.clone();
            handle.on_menu_event(move |app, event| {
                let id = event.id().as_ref().to_string();
                match id.as_str() {
                    "open" => show_console(app, "/console"),
                    "pair" => show_console(app, "/console/phone?pair=1"),
                    "update" => show_console(app, "/console/about"),
                    "pause" => {
                        let paused = app
                            .state::<ShellState>()
                            .snapshot
                            .lock()
                            .map(|s| s.paused_minutes > 0)
                            .unwrap_or(false);
                        if paused {
                            let _ = http(PORT, "DELETE", "/api/push/pause", None);
                        } else {
                            let _ = http(PORT, "POST", "/api/push/pause", Some("{\"minutes\":60}"));
                        }
                        let handle_for_menu = app.clone();
                        let _ = app.run_on_main_thread(move || refresh_tray(&handle_for_menu));
                    }
                    "autostart" => {
                        let launcher = app.autolaunch();
                        let now = launcher.is_enabled().unwrap_or(false);
                        let _ = if now { launcher.disable() } else { launcher.enable() };
                        if let Ok(mut guard) = app.state::<ShellState>().autostart.lock() {
                            *guard = !now;
                        }
                        let handle_for_menu = app.clone();
                        let _ = app.run_on_main_thread(move || refresh_tray(&handle_for_menu));
                    }
                    "quit" => {
                        stop_homingd(app);
                        app.exit(0);
                    }
                    other if other.starts_with("wait:") || other.starts_with("run:") => {
                        show_console(app, "/console")
                    }
                    _ => {}
                }
            });

            // 6) 控制台窗口
            show_console(&menu_handle, "/console");
            println!("[homing] 控制台窗口已请求打开：http://127.0.0.1:{PORT}/console（homingd: {}）", if up { "已就绪" } else { "未就绪" });

            Ok(())
        })
        .on_window_event(|window, event| {
            // 关窗口只是收起来，托盘还在，跟常规菜单栏 App 一样
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .build(tauri::generate_context!())
        .expect("Homing 启动失败");

    app.run(|app, event| {
        if matches!(event, RunEvent::ExitRequested { .. } | RunEvent::Exit) {
            stop_homingd(app);
        }
    });
}
