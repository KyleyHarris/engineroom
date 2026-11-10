import AppKit
import EngineRoomKit
import SwiftUI

/// The window opened by the panel's gear (D-019): a sidebar of the registered projects (pinned
/// first), the repositories, the history and the settings, and on the right whichever is chosen. It
/// has room the panel does not, so a project's worktrees sit side by side, and it stays open while
/// the developer works. It is read while it is open and never while it is closed, as the panel is.
struct MainWindow: View {
    @EnvironmentObject var store: Store

    var body: some View {
        NavigationSplitView {
            Sidebar()
                .navigationSplitViewColumnWidth(min: 190, ideal: 220, max: 300)
        } detail: {
            VStack(spacing: 0) {
                if store.status?.offline == true { OfflineBanner().padding(.top, 8) }
                detail
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background(Color(nsColor: .windowBackgroundColor))
        }
        .font(.app(.body))
        .toolbar {
            ToolbarItemGroup {
                if store.running, store.actionIn == .panel || store.page == .none {
                    Button { store.showRunning() } label: {
                        HStack(spacing: 4) { ProgressView().controlSize(.small); Text(store.planTitle.isEmpty ? "Running…" : store.planTitle).lineLimit(1) }
                    }
                    .help("An action is running; click to see it").pointer()
                } else if let w = store.working {
                    HStack(spacing: 4) { ProgressView().controlSize(.small); Text(w).foregroundColor(.secondary).lineLimit(1) }
                }
                Button { Task { await store.refresh(everything: true) } } label: { Image(systemName: "arrow.clockwise") }
                    .help("Read everything again, the repositories too").pointer()
                QuitButton(icon: true)
            }
        }
        .background(WindowFinder())
        .onAppear {
            store.windowVisible = true
            store.activeSurface = .window
            Task { await store.panelOpened() }
        }
        .onDisappear { store.windowVisible = false }
        .onChange(of: store.windowItem) { item in
            if case .project(let name) = item { Task { await store.openProject(name) } }
            if case .action = store.page, store.actionIn == .window, !store.running { store.close() }
            if store.page != .none, store.actionIn == .window || store.page.isWindowTask { store.page = .none }
        }
        .onReceive(NotificationCenter.default.publisher(for: NSWindow.didBecomeKeyNotification)) { n in
            if let w = n.object as? NSWindow, w === Panel.mainWindow { store.activeSurface = .window }
        }
    }

    @ViewBuilder private var detail: some View {
        if case .action = store.page, store.actionIn == .window {
            ActionView()
        } else {
            switch store.page {
            case .add(let root): AddView(initialRoot: root)
            case .register(let root, let folder): RegisterView(root: root, folder: folder)
            default:
                switch store.windowItem {
                case .project(let name): ProjectPageView(name: name)
                case .repositories: ReposView().padding(.top, 10)
                case .history: HistoryView()
                case .settings: SettingsView()
                case nil:
                    EmptyState(title: "Nothing is chosen", detail: "Choose a project on the left, or the repositories, the history or the settings.")
                        .padding(Gutter.page)
                }
            }
        }
    }
}

extension Store.Page {
    /// A page that belongs to the window: getting or registering a repository.
    var isWindowTask: Bool {
        switch self {
        case .add, .register: return true
        default: return false
        }
    }
}

/// The window's sidebar: the projects, pinned first, then the rest of what the window holds.
struct Sidebar: View {
    @EnvironmentObject var store: Store

    var body: some View {
        List(selection: $store.windowItem) {
            let pinned = store.registeredProjects.filter(\.pinned)
            if store.registeredProjects.isEmpty {
                Section("Projects") { Text("None registered yet").foregroundColor(.secondary) }
            }
            if !pinned.isEmpty {
                Section("Pinned") { ForEach(pinned, id: \.name) { row($0.name, pinned: true) } }
            }
            // The rest by the GitHub owner whose root holds them, and inside that by category.
            ForEach(store.sidebarGroups) { g in
                Section(g.title) {
                    ForEach(g.categories, id: \.category) { c in
                        if g.categories.count > 1 || !c.category.isEmpty {
                            Text(c.category.isEmpty ? "no category" : c.category)
                                .font(.app(.fine, weight: .semibold)).foregroundColor(.secondary)
                        }
                        ForEach(c.projects, id: \.self) { row($0, pinned: false) }
                    }
                }
            }
            Section {
                Label("Repositories", systemImage: "square.stack.3d.up").tag(Store.WindowItem.repositories)
                Label("History", systemImage: "clock").tag(Store.WindowItem.history)
                Label("Settings", systemImage: "gearshape").tag(Store.WindowItem.settings)
            }
        }
        .listStyle(.sidebar)
    }
}

extension Sidebar {
    /// One project in the sidebar: a star when pinned, and the same light as the panel's list
    /// while it has a session: Claude's mark when one waits for the developer, a green dot when
    /// one runs. A project with no session has no light, so the lights that are there stand out.
    /// The star is quiet: pinned is where a project is listed, not something to act on (D-032).
    @ViewBuilder func row(_ name: String, pinned: Bool) -> some View {
        Label {
            HStack {
                Text(name)
                Spacer()
                let state = store.runState(of: name)
                if state != .idle { Light(state: state) }
            }
        } icon: {
            Image(systemName: pinned ? "star.fill" : "folder").foregroundColor(.secondary)
        }
        .tag(Store.WindowItem.project(name))
    }
}

/// Finds the NSWindow the main window is drawn in, so the panel's bookkeeping can tell the two
/// apart (Panel.isPanel).
struct WindowFinder: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView {
        let v = NSView()
        DispatchQueue.main.async { Panel.mainWindow = v.window }
        return v
    }
    func updateNSView(_ nsView: NSView, context: Context) {
        if Panel.mainWindow == nil { DispatchQueue.main.async { Panel.mainWindow = nsView.window } }
    }
}
