import AppKit
import EngineRoomKit
import SwiftUI

/// The menu bar icon: the grid, and Claude's mark beside it while Claude is waiting.
struct MenuBarIcon: View {
    let waiting: Bool
    var body: some View {
        if waiting {
            HStack(spacing: 2) {
                Image(systemName: "square.grid.2x2")
                Text("✻")
            }
            .help("Claude is waiting for you")
        } else {
            Image(systemName: "square.grid.2x2")
        }
    }
}

/// engineroom in the menu bar (issue #29, docs/decisions.md D-008), on the engine or a client: a panel from
/// the menu bar with the sessions, what is done many times a day, and a window opened by its gear
/// with the projects, repositories, history and settings (D-019). It runs nothing itself; every
/// list and every action is a `engineroom api` command, and every action lands in the same history.
@main
struct EngineRoomApp: App {
    @StateObject private var store = Store()

    init() {
        // `--snapshot <dir> [dark]` draws the screens from sample data and exits (Snapshots).
        let args = ProcessInfo.processInfo.arguments
        if let i = args.firstIndex(of: "--snapshot"), i + 1 < args.count {
            let dark = args.dropFirst(i + 2).contains("dark")
            MainActor.assumeIsolated { Snapshots.write(to: args[i + 1], dark: dark) }
            exit(0)
        }
    }

    var body: some Scene {
        MenuBarExtra {
            RootView()
                .environmentObject(store)
                // Wider and shorter than a list of cards, so a project and its checkouts show
                // without scrolling (D-025).
                .frame(width: 840, height: 480)
                // A solid window, like an application's, rather than the menu's see-through
                // material, which shows whatever is behind it and washes the text out.
                .background(Color(nsColor: .windowBackgroundColor))
                // Opened from the menu bar, the panel belongs to an app that is not in front, and
                // macOS draws it as an inactive window: dimmed. It is brought to the front each
                // time it is shown, so it is drawn and answers as the active window.
                .onReceive(NotificationCenter.default.publisher(for: NSWindow.didChangeOcclusionStateNotification)) { n in
                    guard let w = n.object as? NSWindow, Panel.isPanel(w) else { return }
                    let visible = w.occlusionState.contains(.visible)
                    let opened = visible && !store.panelVisible
                    store.panelVisible = visible
                    guard visible else { return }
                    store.activeSurface = .panel
                    NSApp.activate(ignoringOtherApps: true)
                    w.makeKeyAndOrderFront(nil)
                    // Opening the panel is what reads: what it shows is read the moment it opens.
                    if opened { Task { await store.panelOpened() } }
                }
        } label: {
            // The icon carries Claude's mark while Claude waits in any session (APP-FEED-005):
            // the feed keeps the sessions current with the panel closed, so the mark says what is
            // true now, and the developer sees it without opening the panel.
            MenuBarIcon(waiting: PanelList.anyWaiting(store.sessions))
        }
        .menuBarExtraStyle(.window)

        Window("engineroom", id: "main") {
            MainWindow().environmentObject(store)
        }
        .defaultSize(width: 980, height: 700)
    }
}

/// The menu bar panel: the sessions, and an action started from here while it is on screen.
struct RootView: View {
    @EnvironmentObject var store: Store
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(spacing: 0) {
            Header()
            if store.status?.offline == true {
                // The header just above carries the button that hands the work back, so the
                // panel's banner only says the state; the window has no header and keeps it.
                OfflineBanner(button: false)
            }
            if case .action = store.page, store.actionIn == .panel {
                ActionView()
            } else if store.engineDown {
                EngineDownView()
            } else {
                PanelView()
            }
        }
        .font(.app(.body))
        .onAppear {
            // Opening the window always closes the panel first, so the two never sit one over the
            // other.
            store.showWindow = {
                Panel.close()
                openWindow(id: "main")
                NSApp.activate(ignoringOtherApps: true)
                Panel.mainWindow?.makeKeyAndOrderFront(nil)
            }
        }
    }
}

struct Header: View {
    @EnvironmentObject var store: Store

    var body: some View {
        HStack(spacing: 8) {
            Text("engineroom").font(.app(.heading, weight: .bold))
            if let s = store.status {
                Text(s.isClient ? "client of \(store.engineLabel(s))" : "engine · this Mac")
                    .font(.app(.detail))
                    .padding(.horizontal, 7).padding(.vertical, 2)
                    .background(Capsule().fill(Color.secondary.opacity(0.12)))
                if s.offline {
                    Text("WORKING LOCAL").font(.app(.caption, weight: .bold))
                        .padding(.horizontal, 6).padding(.vertical, 2)
                        .background(RoundedRectangle(cornerRadius: Radius.badge).fill(Color.primary))
                        .foregroundColor(Color(nsColor: .windowBackgroundColor))
                }
            }
            Spacer()
            if store.running, case .action = store.page, store.actionIn == .panel {} else if store.running {
                Button { store.showRunning() } label: {
                    HStack(spacing: 4) { ProgressView().controlSize(.small); Text(store.planTitle.isEmpty ? "Running…" : store.planTitle).lineLimit(1) }
                }
                .buttonStyle(.borderless).font(.app(.caption)).help("An action is running; click to see it").pointer()
            } else if let w = store.working {
                ProgressView().controlSize(.small)
                Text(w).font(.app(.caption)).foregroundColor(.secondary).lineLimit(1)
            } else if store.loading {
                ProgressView().controlSize(.small)
            }
            if let s = store.status, s.isClient {
                // A button in words, not a switch (D-032): pressing it shows a handover and changes
                // nothing until that is run, which a switch that looks flipped at once does not
                // say. The badge on the left is what says working local is on. The two names are
                // the app's own for the two directions (D-017).
                Button(s.offline ? "Hand back to the engine" : "Work local") { Task { await store.switchOffline(!s.offline) } }
                    .controlSize(.small).pointer()
                    .help(s.offline
                        ? "Hand the work back to the engine. The handover's commands are shown first."
                        : "Take the work over from the engine and work on this Mac, until it is handed back. Both Macs should be on, so the engine's work is pushed first. The handover's commands are shown first.")
            }
            if let s = store.status, s.isClient, !s.offline {
                // A terminal sends only text, and Claude Code takes a pasted image from the engine's own
                // clipboard; this puts this Mac's clipboard image there (internal/clip).
                Button { Task { await store.sendImage() } } label: { Image(systemName: "photo.on.rectangle.angled") }
                    .buttonStyle(.borderless).help("Send the image on this Mac's clipboard to the engine, then paste it in the session").pointer()
            }
            Button { Task { await store.refresh(everything: true) } } label: { Image(systemName: "arrow.clockwise") }.pointer()
                .buttonStyle(.borderless).help("Read everything again, the repositories too")
            Button {
                if store.windowItem == nil { store.windowItem = store.registeredProjects.first.map { .project($0.name) } ?? .repositories }
                store.showWindow?()
            } label: { Image(systemName: "gearshape") }.pointer()
                .buttonStyle(.borderless).help("Projects, repositories, history and settings")
            // Quit stands apart from refresh and the gear (D-032): it is the one icon here that
            // ends something, and a rule and a wider gap keep a click meant for the gear off it.
            Divider().frame(height: 14).padding(.horizontal, 3)
            QuitButton(icon: true).buttonStyle(.borderless)
        }
        .padding(.horizontal, Gutter.panel).padding(.top, 10).padding(.bottom, 8)
    }
}

/// Says this Mac is working local, on every screen while it is. It carries the button that hands
/// the work back unless the screen already has one in sight (the panel's header does), and sits
/// at the gutter of the surface it is on, so it lines up with what is under it.
struct OfflineBanner: View {
    @EnvironmentObject var store: Store
    var button = true
    var gutter = Gutter.panel
    var body: some View {
        Banner(tone: .attention, text: "Working local: this Mac has the work. Hand it back to the engine when you are done.") {
            if button {
                Button("Hand back to the engine") { Task { await store.switchOffline(false) } }.controlSize(.small).pointer()
            }
        }
        .padding(.horizontal, gutter).padding(.bottom, 6)
    }
}

/// The engine did not answer: retry, or work offline (CONN-DOWN, CONN-OFFLINE-001). Drawn as the
/// empty screen every other one is, marked as a failure: what happened, what it means for the work,
/// why in the engine's own words, and the two ways on side by side, Retry filled as the one tried
/// first.
struct EngineDownView: View {
    @EnvironmentObject var store: Store
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            EmptyState(tone: .bad, title: "The engine did not answer.",
                       detail: "Nothing has been started on either machine. Sessions already running on the engine keep running.") {
                VStack(alignment: .leading, spacing: 10) {
                    // The reason as it was given, selectable so it can be copied into a search.
                    if let p = store.problem, !p.isEmpty {
                        Text(p).font(.app(.detail, design: .monospaced)).foregroundColor(.secondary).textSelection(.enabled)
                    }
                    HStack(spacing: 8) {
                        Button("Retry the engine") { Task { await store.refresh(everything: true) } }.buttonStyle(.borderedProminent).pointer()
                        Button("Work local on this Mac") { Task { await store.switchOffline(true) } }.pointer()
                    }
                }
            }
            Spacer()
        }
        .padding(Gutter.panel)
    }
}

/// The bar at the bottom of an action's page: its buttons stay in sight while its commands scroll,
/// at the gutter of the surface it is on, so they line up with the commands above them.
struct Footer<Content: View>: View {
    var gutter = Gutter.panel
    @ViewBuilder var content: Content
    var body: some View {
        VStack(spacing: 0) {
            Divider()
            HStack(spacing: 8) { content }.padding(.horizontal, gutter).padding(.vertical, 10)
        }
    }
}

/// Quits engineroom: the power icon in the panel's header, and the button at the foot of Settings.
/// The panel's is one click from the menu bar, as a menu bar app's Quit is expected to be, and
/// stands apart from the icons beside it, in the header and in the window's toolbar (D-032).
/// Command-Q quits from the panel or the window (the power icon is in both): the app has no menu
/// bar of its own (LSUIElement), so the shortcut sits on those buttons rather than on a menu item
/// nothing shows. The one in Settings has none, so the window does not carry the shortcut twice. An
/// action's commands run as this app's children and report to it, so quitting while one runs may
/// stop it before it finishes: then it asks first.
struct QuitButton: View {
    @EnvironmentObject var store: Store
    /// The power icon (the panel's header and the window's toolbar), with Command-Q, rather than
    /// the words (Settings).
    var icon = false
    @State private var asking = false

    var body: some View {
        Button {
            if store.running { asking = true } else { NSApplication.shared.terminate(nil) }
        } label: {
            if icon { Image(systemName: "power") } else { Text("Quit engineroom") }
        }
        .keyboardShortcut(icon ? KeyboardShortcut("q", modifiers: .command) : nil)
        .help(icon ? "Quit engineroom (⌘Q)" : "Quit engineroom")
        .pointer()
        .confirmationDialog("An action is running: \(store.planTitle.isEmpty ? "untitled" : store.planTitle)", isPresented: $asking) {
            Button("Quit anyway", role: .destructive) { NSApplication.shared.terminate(nil) }
            Button("Keep it running", role: .cancel) {}
        } message: {
            Text("Its commands run as part of this app, so quitting now may stop it before it finishes.")
        }
    }
}

/// The back link at the top of a page reached from another, at the gutter of the surface it is on.
struct BackLink: View {
    @EnvironmentObject var store: Store
    var label = "Back"
    var gutter = Gutter.panel
    var body: some View {
        HStack {
            Button("‹ \(label)") { store.close() }.buttonStyle(.link).pointer()
            Spacer()
        }
        .padding(.horizontal, gutter).padding(.bottom, 4)
    }
}

/// The menu bar panel: the app's one window apart from the status bar's own, which holds the
/// menu bar icon.
enum Panel {
    /// The main window, which is not the panel; set when it appears (MainWindow).
    static weak var mainWindow: NSWindow?

    static func isPanel(_ w: NSWindow) -> Bool {
        !w.className.contains("StatusBar") && w !== mainWindow && w.identifier?.rawValue.hasPrefix("main") != true
    }

    /// Closes the panel, as clicking elsewhere does, so another application's window can come to
    /// the front.
    @MainActor static func close() {
        for w in NSApp.windows where w.isVisible && isPanel(w) {
            w.orderOut(nil)
        }
    }
}

extension View {
    /// Shows the pointing hand over something clickable, as a link does in a browser; SwiftUI on
    /// macOS keeps the arrow over buttons.
    func pointer() -> some View {
        onHover { inside in
            if inside { NSCursor.pointingHand.push() } else { NSCursor.pop() }
        }
    }
}
