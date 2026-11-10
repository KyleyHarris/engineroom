import AppKit
import Foundation
import EngineRoomKit
import ServiceManagement

/// Everything the window shows, read from engineroom. The store never decides anything itself: it asks
/// engineroom and shows the answer, so the window and the terminal interface always agree.
@MainActor
final class Store: ObservableObject {
    /// What fills a surface instead of its usual content, for one task: an action's commands
    /// (in the panel or the window, whichever it was started from), or, in the window, getting
    /// or registering a repository.
    enum Page: Equatable {
        case none
        case add(root: String?)
        case register(root: String, folder: String)
        case action(title: String)
    }

    /// The app's two surfaces: the menu bar panel, which holds the sessions, and the window, which
    /// holds the projects, repositories, history and settings.
    enum Surface { case panel, window }

    /// What the window's sidebar has chosen.
    enum WindowItem: Hashable {
        case project(String)
        case repositories, history, settings
    }

    let client: EngineRoomClient?
    @Published var page: Page = .none
    /// The window's sidebar choice; a project is shown by default once one is known.
    @Published var windowItem: WindowItem?
    /// Whether the window is open, so it is read only then, as the panel is.
    @Published var windowVisible = false
    /// The surface last in front, where an action asked for now is shown.
    var activeSurface: Surface = .panel
    /// The monitor that sets activeSurface from each click; kept for the app's life.
    private var clickMonitor: Any?
    /// The monitor that gives the panel and the window their keys (key(_:in:), D-033); kept for
    /// the app's life.
    private var keyMonitor: Any?
    /// Where the pointer was when an arrow key last chose a project in the panel. A pointer that
    /// has not moved since chooses nothing, so a row resting under it does not take the choice
    /// back from the keys; the first movement gives pointing its say again (pointerChooses).
    private var keyPointer: NSPoint?
    /// Counts the arrow keys that chose a project in the panel. The list brings the chosen row
    /// into sight each time this changes, and only then: a row chosen by pointing is in sight
    /// already, and must not slide under the pointer that chose it.
    @Published private(set) var keySteps = 0
    /// Where the action on screen was started, and so where its commands are shown.
    @Published var actionIn: Surface = .panel
    /// Opens the window and brings it to the front; set by the panel, which can reach SwiftUI's
    /// openWindow.
    var showWindow: (() -> Void)?
    @Published var status: Status?
    @Published var sessions: [Session] = []
    /// Each checkout of the pinned projects and those with a running session, by project, as the
    /// feed last sent them (D-025): the panel's cards, running or not.
    @Published var checkouts: [String: [CheckoutFacts]] = [:]
    /// The project the panel shows on its right: the one last pointed at or clicked, kept while the
    /// panel is closed and across a restart of the app, so the panel opens where the developer was,
    /// and comes back to it after an action. A per-machine convenience only.
    @Published var panelSelected: String? = UserDefaults.standard.string(forKey: "panelSelected") {
        didSet { UserDefaults.standard.set(panelSelected, forKey: "panelSelected") }
    }
    @Published var sessionsRead: String?
    @Published var repos: ReposReply?
    @Published var history: [HistoryEntry] = []
    @Published var problem: String?
    @Published var engineDown = false
    @Published var loading = false

    // The action on screen: its commands before it runs, then each step as it runs.
    @Published var pendingAction: [String] = []
    @Published var planLines: [String] = []
    @Published var planTitle = ""
    @Published var planMachine = ""
    @Published var planNotes: [String] = []
    /// The plan on screen cannot run as asked (a handover that cannot be done in full); its notes
    /// say what is in the way.
    @Published var planBlocked = false
    @Published var planOffers: [Offer] = []
    @Published var stepStatus: [Int: String] = [:]
    @Published var stepOutput: [Int: String] = [:]
    @Published var stepFail: [Int: String] = [:]
    @Published var running = false
    @Published var finished: Bool?
    @Published var actionError: String?
    @Published var copied: String?

    /// The forwarder this app started (`engineroom forward --watch`), while forwarding is on. Only this
    /// process is ever stopped by the app: it is the one the app started.

    /// The open project's page as the engine (or this Mac) last read it, and why it could not be.
    @Published var project: ProjectPage?
    @Published var projectError: String?
    /// On a client: what the forwarder last found and did, so a port that is not reachable here
    /// says why.
    @Published var forwarding: ForwardStatus?
    /// What the app is doing in the background right now, said in the header with a spinner, so a
    /// click that takes a moment never looks like nothing happened.
    @Published var working: String?
    /// The forwarder's watch this app started (`engineroom forward --watch`), while forwarding is on.
    private var watcher: Process?
    /// The `engineroom sweep --watch` this app started (keepSweeper), or nil.
    private var sweeper: Process?
    /// The feed this app keeps open while it runs (keepFeed, D-023): the sessions, sent by the engine
    /// (or this Mac, working local) when they change. Only this process is ever stopped by the app.
    private var feed: Feed?
    /// Which feed is the current one: a feed stopped on purpose ends after its successor began,
    /// and what it says then is ignored.
    private var feedGeneration = 0
    /// Whether the current feed has sent a list, so the sessions shown are its and a refresh asks
    /// it to look rather than reading them over a connection of its own.
    private var feedLive = false
    /// Whether the current feed was started working local, so switching starts it again where the
    /// sessions now are.
    private var feedOffline: Bool?
    /// Feeds in a row that ended at once (an engine that does not answer, or one too old to have a
    /// feed), and when the next may start: each waits longer, up to five minutes, so an
    /// unreachable engine is not asked again every few seconds.
    private var feedFailures = 0
    private var feedRetryAt: Date?
    /// Whether the panel is on screen. The app reads its own state and syncs only while it is (or
    /// the window is); apart from that only the feed, the forwarder's watch while anything is
    /// forwarded, and the sweep where this machine's sessions are its own, run.
    @Published var panelVisible = false
    private var refreshing = false
    private var refreshAgain = false
    private var refreshAgainEverything = false
    /// When the repositories were last read. Listing them asks GitHub once per root, and they
    /// change rarely, so a read leaves them alone for an hour; the refresh button and an action
    /// that ran read them at once.
    private var reposReadAt: Date?
    static let reposMaxAge: TimeInterval = 3600
    /// The pin a project was just set to, shown on its switch at once while the change is made
    /// on the engine; cleared when the page is read again.
    @Published var pendingPin: [String: Bool] = [:]

    /// Your settings folder as a repository (`engineroom settings status`), and what the last sync said.
    @Published var settingsStatus: String = ""
    @Published var settingsSync: String = ""
    @Published var settingsSyncing = false

    /// A store with no connection to engineroom, holding only what a snapshot sets (Snapshots): it reads
    /// nothing, starts nothing and watches for nothing.
    init(sample: Bool) {
        client = nil
        // A snapshot shows the sample's own first project, never one remembered from use.
        panelSelected = nil
    }

    init() {
        if let path = EngineRoomClient.locate() {
            client = EngineRoomClient(executable: path)
        } else {
            client = nil
            problem = "engineroom was not found. Install it with scripts/install.sh, or set ENGINEROOM_PATH."
        }
        // An action is shown where it was asked for: the window or the panel the click was in.
        // Every click is seen here before the button it lands on acts, so the surface is the one
        // clicked, whichever window macOS last told about. Following which window became key missed
        // clicks in a window that was already key, and the window's Restart then showed its
        // commands in the closed panel, so nothing seemed to happen.
        clickMonitor = NSEvent.addLocalMonitorForEvents(matching: [.leftMouseDown]) { [weak self] event in
            // Local monitors are called on the main thread, where the store lives.
            if let w = event.window, !w.className.contains("StatusBar") {
                let surface: Surface = Panel.isPanel(w) ? .panel : .window
                MainActor.assumeIsolated { self?.activeSurface = surface }
            }
            return event
        }
        // The keys of the panel's list and Escape on a page reached from another (D-033). A key
        // that is acted on is taken, so it does not also sound the "nothing takes this key" beep;
        // every other key goes on to the window untouched.
        keyMonitor = NSEvent.addLocalMonitorForEvents(matching: [.keyDown]) { [weak self] event in
            guard let self, let w = event.window else { return event }
            return MainActor.assumeIsolated { self.key(event, in: w) } ? nil : event
        }
        // Read the status once at launch, so the feed and, if forwarding is on, the forwarder run
        // from the moment the app does; everything else waits for the panel to open
        // (panelOpened). What this app started is stopped when the app quits. A feed that ends is
        // written to no more: SIGPIPE would otherwise end the app on such a write.
        signal(SIGPIPE, SIG_IGN)
        Task { [weak self] in
            guard let self, let client = self.client else { return }
            self.status = try? await client.status()
            await self.alignForwards()
            self.keepWatches()
        }
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification, object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.stopWatches() }
        }
        // Waking from sleep, the connection to the engine and its forwards may be gone: bring the
        // feed and the forwards back at once, and read the lists again only if the panel is open
        // to show them.
        NSWorkspace.shared.notificationCenter.addObserver(forName: NSWorkspace.didWakeNotification, object: nil, queue: .main) { [weak self] _ in
            Task { @MainActor in
                guard let self else { return }
                self.feedRetryAt = nil
                self.keepFeed()
                if self.panelVisible { await self.refresh() }
                await self.alignForwards()
            }
        }
    }

    /// The panel has just opened: read what it shows, and, when the last were more than an hour
    /// ago, sync the settings, keep this Mac ready to work local, and compare the engine's version.
    /// Nothing but the feed runs while the panel is closed, so these happen at most hourly, and
    /// only when used.
    func panelOpened() async {
        // Opening it is the developer looking: a feed waiting to try the engine again tries now.
        feedRetryAt = nil
        await refresh()
        await alignForwards()
        if let last = hourlyAt, Date().timeIntervalSince(last) < 3600 { return }
        hourlyAt = Date()
        await readEngineVersion()
        await syncSettings()
        await keepMirror()
    }
    private var hourlyAt: Date?

    /// Reads what the tabs show: history, settings and forwarding every time; the sessions over a
    /// connection of its own only while the feed has none to send, and otherwise by asking the feed
    /// to look now; the repositories only when asked for (`everything`) or an hour after they were
    /// last read. It runs when the panel or window opens, after an action and on the refresh
    /// button, never on a timer; what changes on the engine by itself comes through the feed.
    func refresh(everything: Bool = false) async {
        // One read at a time: the panel opening and an action's own refresh would otherwise
        // stack reads on top of each other whenever one is slow.
        // One asked for while one runs is not dropped: the running one reads again once more when
        // it finishes, so what an action changed always shows.
        guard let client else { return }
        if refreshing {
            refreshAgain = true
            refreshAgainEverything = refreshAgainEverything || everything
            return
        }
        refreshing = true
        defer {
            refreshing = false
            if refreshAgain {
                let all = refreshAgainEverything
                refreshAgain = false
                refreshAgainEverything = false
                Task { await refresh(everything: all) }
            }
        }
        loading = true
        defer { loading = false }
        do {
            status = try await client.status()
        } catch {
            problem = error.localizedDescription
            return
        }
        if everything { feedRetryAt = nil }
        keepWatches()
        if feedLive, let feed, feed.isRunning {
            // What an action changed is sent by the feed within a moment, over its connection.
            feed.look()
            problem = nil
        } else {
            do {
                let s = try await client.sessions()
                sessions = s.sessions
                checkouts = s.checkouts ?? [:]
                sessionsRead = s.read
                problem = nil
                engineDown = false
            } catch let e as APIError {
                problem = e.error
                engineDown = e.engineDown ?? false
                sessions = []
            } catch {
                problem = error.localizedDescription
            }
        }
        if !engineDown, everything || repos == nil || reposReadAt.map({ Date().timeIntervalSince($0) >= Self.reposMaxAge }) ?? true {
            await readRepos()
        }
        history = (try? await client.history()) ?? []
        await readSettingsStatus()
        if windowVisible, case .project(let name) = windowItem { await loadProject(name) }
        await readForwarding()
    }

    /// Reads the repositories list, and notes when, so the timed read can leave it for an hour.
    func readRepos() async {
        guard let client else { return }
        if let r = try? await client.repos() {
            repos = r
            reposReadAt = Date()
        }
    }

    /// The pinned projects, by name, from the repositories list (in a root or not), in name order.
    var pinnedProjects: [String] {
        guard let r = repos else { return [] }
        var names = Set<String>()
        for l in r.roots { for repo in l.repos where repo.isRegistered && repo.pinned == true { names.insert(repo.project ?? repo.name) } }
        for p in r.projects where p.pinned == true { names.insert(p.name) }
        return names.sorted { $0.lowercased() < $1.lowercased() }
    }

    /// What the panel lists, laid out from the feed's sessions and the pinned projects.
    var panelList: PanelList { PanelList(sessions: sessions, pinned: pinnedProjects) }

    /// The panel's rows in the order it draws them, the project last chosen kept in the list. The
    /// panel draws from this and the keys walk it, so a key always moves to the row that is next
    /// on screen.
    var panelProjects: [PanelProject] { PanelProject.list(panelList, checkouts: checkouts, keep: panelSelected) }

    // MARK: the keyboard (D-033)

    /// Acts on a key pressed in the panel or the window, and says whether it did. Which key does
    /// what, where, is PanelKey.effect; this reads the place off the window and carries the
    /// effect out. A key in any window that is neither the panel nor the main window is left alone.
    func key(_ event: NSEvent, in window: NSWindow) -> Bool {
        guard let key = PanelKey(keyCode: event.keyCode) else { return false }
        let inPanel: Bool
        if window === Panel.mainWindow {
            inPanel = false
        } else if Panel.isPanel(window), window.isVisible {
            inPanel = true
        } else {
            return false
        }
        let screen: PanelKey.Screen
        if case .action = page, actionIn == (inPanel ? .panel : .window) {
            screen = .action
        } else if inPanel {
            screen = engineDown ? .engineDown : .list
        } else {
            screen = page.isWindowTask ? .task : .other
        }
        let place = PanelKey.Place(
            inPanel: inPanel, screen: screen,
            modified: !event.modifierFlags.intersection([.command, .option, .control, .shift]).isEmpty,
            // A dialog over the window, the dialog's own window, or one that stops the whole app.
            dialog: window.attachedSheet != nil || window.sheetParent != nil || NSApp.modalWindow != nil,
            typing: (window.firstResponder as? NSText)?.isEditable == true,
            composing: (window.firstResponder as? NSTextView)?.hasMarkedText() == true,
            repeated: event.isARepeat)
        switch key.effect(at: place) {
        case .pass:
            return false
        case .taken:
            return true
        case .back:
            close()
            return true
        case .closePanel:
            Panel.close()
            return true
        case .step(let rows):
            // With no rows (only this Mac's own sessions, or nothing) there is nowhere to go, and
            // the key is left to the window.
            guard let next = PanelProject.step(panelProjects, from: panelSelected, by: rows) else { return false }
            keyPointer = NSEvent.mouseLocation
            if next != panelSelected { panelSelected = next }
            keySteps += 1
            return true
        case .act:
            guard let project = PanelProject.shown(panelProjects, chosen: panelSelected) else { return false }
            // The key was pressed in the panel, so its action is shown in the panel.
            activeSurface = .panel
            let mode = status?.start ?? "zellij"
            switch project.keyAction {
            case .join(let s):
                Task { await runNow(["join"] + s.target, opensWindow: true) }
            case .start(let action):
                Task { await runNow(action + ["--mode", mode], opensWindow: mode != "remote") }
            }
            return true
        }
    }

    /// Whether pointing at a row of the panel's list chooses it now: always, except while the
    /// pointer has not moved since an arrow key chose a row. The list can scroll under a still
    /// pointer when a key moves the choice, and that must not choose the row that arrives there.
    func pointerChooses() -> Bool {
        guard let at = keyPointer else { return true }
        if at == NSEvent.mouseLocation { return false }
        keyPointer = nil
        return true
    }

    /// Opens a project's page, in the window.
    func openProject(_ name: String) async {
        // A session that belongs to no registered project has no page to open.
        guard !name.isEmpty else { return }
        windowItem = .project(name)
        if case .action = page, actionIn == .panel {} else { page = .none }
        showWindow?()
        if project?.name != name { project = nil }
        await loadProject(name)
    }

    /// What a project's sessions are doing, as one light for the whole project: what the window's
    /// sidebar and the repositories list show beside its name.
    func runState(of project: String) -> RunState { RunState(PanelLight.of(project: project, in: sessions)) }

    /// One group of the window's sidebar: the registered projects of one GitHub owner's root (or
    /// in no root), not pinned, by category, categories in name order and those with none last.
    struct SidebarGroup: Identifiable {
        let title: String
        let categories: [(category: String, projects: [String])]
        var id: String { title }
    }

    /// The sidebar's groups below Pinned.
    var sidebarGroups: [SidebarGroup] {
        guard let r = repos else { return [] }
        func pinned(_ name: String, _ was: Bool?) -> Bool { pendingPin[name] ?? (was == true) }
        func byCategory(_ items: [(String, String)]) -> [(category: String, projects: [String])] {
            let groups = Dictionary(grouping: items, by: { $0.1 })
            let named = groups.keys.filter { !$0.isEmpty }.sorted { $0.lowercased() < $1.lowercased() }
            return (named + (groups[""] == nil ? [] : [""])).map { c in
                (category: c, projects: groups[c]!.map(\.0).sorted { $0.lowercased() < $1.lowercased() })
            }
        }
        var out: [SidebarGroup] = []
        for l in r.roots {
            let items = l.repos.filter { $0.isRegistered && !pinned($0.project ?? $0.name, $0.pinned) }
                .map { ($0.project ?? $0.name, $0.category ?? "") }
            if !items.isEmpty { out.append(SidebarGroup(title: l.root.owner, categories: byCategory(items))) }
        }
        let loose = r.projects.filter { !pinned($0.name, $0.pinned) }.map { ($0.name, $0.group ?? "") }
        if !loose.isEmpty { out.append(SidebarGroup(title: "In no root", categories: byCategory(loose))) }
        return out
    }

    /// Every registered project, by name, with whether it is pinned: the window's sidebar.
    var registeredProjects: [(name: String, pinned: Bool)] {
        guard let r = repos else { return [] }
        var seen: [String: Bool] = [:]
        for l in r.roots { for repo in l.repos where repo.isRegistered { seen[repo.project ?? repo.name] = repo.pinned == true } }
        for p in r.projects { seen[p.name] = p.pinned == true }
        return seen.map { (name: $0.key, pinned: pendingPin[$0.key] ?? $0.value) }
            .sorted { $0.pinned != $1.pinned ? $0.pinned : $0.name.lowercased() < $1.name.lowercased() }
    }

    /// On a client that can reach the engine: copies here the engine's projects this Mac lacks and
    /// fetches the rest (`engineroom mirror`), so working local finds everything here. Hourly, with the
    /// settings sync; it pushes nothing and moves no worktree, and its commands are in the history.
    func keepMirror() async {
        guard let client, let s = status, s.isClient, !s.offline else { return }
        _ = try? await client.output(["api", "run", "mirror"])
    }

    /// Runs a small action that needs no page of its own (pinning, a category), staying where the
    /// developer is, then reads everything again so the change shows at once; on the project page
    /// too. Its commands are in the history like every other action's; a failure is said.
    func runQuietly(_ action: [String], saying: String) async {
        guard let client else { return }
        working = saying
        defer { working = nil }
        let ok = await client.run(action) { _ in }
        if !ok { problem = "\(action.joined(separator: " ")) did not finish; History, in the window, has its commands and what they said." }
        // Only what the change shows on: the open project's page and the repositories (the
        // pinned list), not the whole chain refresh() reads.
        if case .project(let name) = windowItem { await loadProject(name) }
        await readRepos()
        pendingPin = [:]
    }

    /// Puts the image on this Mac's clipboard on the engine's clipboard, so pasting in a session on the
    /// engine pastes it. Says when it is there, or why not (no image copied, the engine not answering).
    func sendImage() async {
        guard let client else { return }
        working = "Sending the clipboard's image to the engine…"
        defer { working = nil }
        var said = ""
        let ok = await client.run(["send-image"]) { e in
            if e.status == "failed", let o = e.output { said = o }
            if let err = e.error { said = err }
        }
        let reason = said.split(separator: "\n").last.map(String.init) ?? "History, in the window, has its commands and what they said"
        problem = ok ? nil : "The image was not sent: \(reason)"
        imageSent = ok ? Date() : nil
    }

    /// When an image was last sent to the engine, so the panel can say to paste it now.
    @Published var imageSent: Date?

    /// Pins a project or unpins it: the switch shows the new state at once, and the header says
    /// it is being saved until the engine has it.
    func setPinned(_ project: String, _ on: Bool) async {
        pendingPin[project] = on
        await runQuietly(["pin", project, on ? "on" : "off"], saying: on ? "Pinning \(project)…" : "Unpinning \(project)…")
    }

    func loadProject(_ name: String) async {
        guard let client else { return }
        do {
            project = try await client.json(ProjectPage.self, ["api", "project", name])
            projectError = nil
        } catch {
            projectError = error.localizedDescription
        }
    }

    /// On a client, the engine's version when it differs from this Mac's, so a mismatch is said
    /// with the fix instead of showing up as answers the app cannot read.
    @Published var engineVersionDiffers: String?
    /// The engine's own machine name, from the same read (CONN-ROLE-003). Kept while working local,
    /// since the engine is still that machine; nil until the engine has given one.
    @Published var engineName: String?
    /// Which machine the version check found behind ("here", "engine", "apart", "unknown" or nil),
    /// so the banner offers to upgrade that one.
    @Published var engineBehind: String?

    /// How the panel and Settings name the engine on a client: its own machine name when it gave
    /// one, else the SSH name this Mac reaches it by.
    func engineLabel(_ s: Status) -> String {
        engineName ?? s.engine ?? "the engine"
    }

    func readEngineVersion() async {
        guard let client, status?.isClient == true, status?.offline != true else { engineVersionDiffers = nil; return }
        if let v = try? await client.json(EngineVersion.self, ["api", "engine-version"]) {
            engineVersionDiffers = v.advice
            if let name = v.name, !name.isEmpty { engineName = name }
            engineBehind = v.behind
        }
    }

    /// Reads the forwarder's last pass, on a client.
    func readForwarding() async {
        guard let client, status?.isClient == true else { forwarding = nil; return }
        if let (data, code) = try? await client.output(["forward", "status", "--json"]) {
            forwarding = try? EngineRoomClient.decode(ForwardStatus.self, data: data, code: code)
        }
    }

    /// Leaves one session's ports out of forwarding, or puts them back, then checks again.
    /// One session's forwarding switch (`engineroom forward add|remove`): its ports reachable on this
    /// Mac or not. With nothing switched on, and forwarding every session off, the connection to
    /// the engine is closed and the forwarder stops, so nothing runs for nothing.
    func setSessionForward(_ session: String, _ on: Bool) async {
        guard let client else { return }
        working = on ? "Forwarding \(session)'s ports…" : "Stopping \(session)'s forwards…"
        defer { working = nil }
        _ = try? await client.output(["forward", on ? "add" : "remove", session])
        status = (try? await client.status()) ?? status
        keepWatches()
        await readForwarding()
    }

    /// Whether a session's ports are to be forwarded to this Mac.
    func forwards(_ session: String) -> Bool { forwarding?.wants(session) == true }

    /// Opens one of a session's links. First it asks the machine the stack runs on (the engine,
    /// over SSH, from a client working on it) whether anything answers on the link's port, and when
    /// nothing does it starts the session's app, shown as any action is, and opens the link once it
    /// is up; a failed start stays on screen and opens nothing. On a client working on the engine
    /// whose port is not forwarded yet, it switches that session's forwarding on first, so the link
    /// works with one click.
    func open(_ link: EngineRoomKit.Link, of session: String?) async {
        guard let url = URL(string: link.url) else { return }
        if let session, let port = url.port, let client {
            let serve = ["serve", session, String(port), "--mode", status?.start ?? "zellij"]
            working = "Checking \(session)'s app answers on port \(port)…"
            let needed = (try? await client.plan(serve))?.lines.isEmpty == false
            working = nil
            if needed {
                await runNow(serve)
                guard finished == true else { return }
                resetAction()
                page = .none
            }
        }
        if let session, status?.isClient == true, status?.offline != true,
           let port = url.port, forwarding?.forwarded[String(port)] != session {
            await setSessionForward(session, true)
        }
        NSWorkspace.shared.open(url)
    }

    /// Whether a checkout's folder is on this Mac: on the engine, or on a client working local.
    /// A client working through the engine has its folders there, so it shows no Finder button.
    var filesHere: Bool { status.map { !$0.isClient || $0.offline } ?? false }

    /// Shows a checkout's folder in Finder.
    func showInFinder(_ path: String) {
        NSWorkspace.shared.open(URL(fileURLWithPath: path, isDirectory: true))
    }

    /// Reads what `engineroom settings status` says, as its own lines.
    func readSettingsStatus() async {
        guard let client, let (data, _) = try? await client.output(["settings", "status"]) else { return }
        settingsStatus = String(decoding: data, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Syncs your settings repository (`engineroom settings sync`): commits this Mac's changes, takes
    /// other machines', sends this one's. Only when it is a repository with a remote; the sync's
    /// commands are in the history like every other action.
    func syncSettings() async {
        guard let client, !settingsSyncing else { return }
        working = "Syncing your settings…"
        defer { working = nil }
        await readSettingsStatus()
        guard settingsStatus.contains("repository: "), !settingsStatus.contains("repository: none") else { return }
        settingsSyncing = true
        defer { settingsSyncing = false }
        if let (data, code) = try? await client.output(["settings", "sync"]) {
            let text = String(decoding: data, as: UTF8.self)
            let when = Date().formatted(date: .omitted, time: .shortened)
            let last = text.split(separator: "\n").last.map(String.init) ?? "run engineroom settings sync in a terminal to see why"
            settingsSync = code == 0 ? "Synced at \(when)" : "Sync failed at \(when): \(last)"
        }
        await readSettingsStatus()
        history = (try? await client.history()) ?? []
    }

    // MARK: actions

    /// Shows an action's commands before anything runs (APP-PREVIEW-001).
    func preview(_ action: [String]) async {
        guard let client else { return }
        // One action at a time: while one runs, asking for another shows the running one, so two
        // runs never write into one screen.
        if running { showRunning(); return }
        resetAction()
        actionIn = activeSurface
        page = .action(title: "")
        pendingAction = action
        do {
            let p = try await client.plan(action) { [weak self] step in self?.noteProgress(step) }
            planTitle = p.plan.title
            planMachine = p.plan.machine ?? ""
            planNotes = p.plan.notes ?? []
            planOffers = p.plan.offers ?? []
            planBlocked = p.plan.blocked ?? false
            planLines = p.lines
        } catch {
            actionError = error.localizedDescription
            // A refusal that something would make safe comes with what would (D-024), offered
            // under it as any plan's offers are.
            if let e = error as? APIError, let offers = e.offers { planOffers = offers }
        }
    }

    /// Runs the action whose commands are on screen.
    func runPending() async {
        guard let client, !pendingAction.isEmpty, !running else { return }
        running = true
        finished = nil
        stepStatus = [:]
        stepOutput = [:]
        planProgress = []
        let action = pendingAction
        let ok = await client.run(action) { [weak self] e in
            self?.apply(e)
        }
        running = false
        finished = ok
        // An action may have cloned, registered or removed a repository: read those too.
        await refresh(everything: true)
        // On a client that forwards, bring the forwards in line at once, so a stack or session
        // just started is reachable here now.
        await alignForwards()
    }

    /// Runs an action straight away, showing its commands as they run: for actions that create
    /// nothing worth confirming (joining, opening the editor, starting).
    ///
    /// opensWindow is for an action whose point is another application's window: a terminal
    /// joined to a session, or VS Code. The panel closes as it starts, so that window comes to the
    /// front, and when the action succeeds the panel is back at the list the next time it opens.
    /// A failure stays on the action page, so reopening the panel shows why.
    func runNow(_ action: [String], opensWindow: Bool = false) async {
        if running { showRunning(); return }
        resetAction()
        actionIn = activeSurface
        page = .action(title: "")
        pendingAction = action
        if opensWindow { Panel.close() }
        await runPending()
        if opensWindow && finished == true {
            resetAction()
            page = .none
        }
    }

    /// Shows the action that is running, on the surface it was started from.
    func showRunning() {
        page = .action(title: planTitle)
        if actionIn == .window { showWindow?() }
    }

    private func apply(_ e: RunEvent) {
        switch e.event {
        case "plan":
            if let p = e.plan { planTitle = p.title; planMachine = p.machine ?? ""; planNotes = p.notes ?? []; planOffers = p.offers ?? []; planBlocked = p.blocked ?? false }
            planLines = e.lines ?? []
        case "step":
            if let i = e.index {
                stepStatus[i] = e.status
                if let o = e.output, !o.isEmpty { stepOutput[i] = o }
                if let f = e.onFail, !f.isEmpty { stepFail[i] = f }
            }
        case "done":
            if let err = e.error { actionError = err }
        case "progress":
            if let p = e.progress { noteProgress(p) }
        default: break
        }
    }

    /// How far each machine's reading has got while the plan is made, one per machine in the order
    /// they started, so the waiting screen shows a meter for each.
    @Published var planProgress: [PlanProgress] = []

    /// Keeps the latest step of each machine's reading. A step carries no command when it comes
    /// from this machine, so the command already shown for that machine stays.
    func noteProgress(_ p: PlanProgress) {
        if let i = planProgress.firstIndex(where: { $0.side == p.side }) {
            let had = planProgress[i]
            planProgress[i] = PlanProgress(side: p.side, done: p.done, total: p.total, project: p.project, command: p.command ?? had.command)
        } else {
            planProgress.append(p)
        }
    }

    private func resetAction() {
        pendingAction = []
        planProgress = []
        planLines = []
        planTitle = ""
        planMachine = ""
        planNotes = []
        planBlocked = false
        planOffers = []
        stepStatus = [:]
        stepOutput = [:]
        stepFail = [:]
        running = false
        finished = nil
        actionError = nil
        copied = nil
    }

    func close() {
        page = .none
        // Leaving a running action only leaves its page: it keeps running, the header says so,
        // and clicking that returns to it.
        if !running { resetAction() }
    }

    // MARK: settings

    func set(_ key: String, _ value: String) async {
        guard let client else { return }
        do {
            status = try await client.set(key, value)
        } catch {
            problem = error.localizedDescription
        }
        await refresh()
        // Turning forwarding on brings the forwards up at once (off has closed them already).
        if key == "forward" { await alignForwards() }
        keepWatches()
    }

    /// Switches this Mac offline or back online, then shows the handoff in that direction: what
    /// one machine holds that the other does not, and the commands that carry it across GitHub,
    /// run only on Run (CONN-HANDOFF-001, 004).
    func switchOffline(_ on: Bool) async {
        // The switch is the handover's last command: shown first, run only on Run, so Back changes
        // nothing.
        await preview(["handoff", on ? "from-engine" : "to-engine", "--switch"])
    }

    /// Turns forwarding on or off (D-013). On, the forwards are brought in line with the engine's
    /// running sessions at once, and again whenever alignForwards runs; off closes every forward.
    func setForward(_ on: Bool) async {
        await set("forward", on ? "on" : "off")
    }

    /// Brings this Mac's forwards in line with the engine's running sessions, once (`engineroom
    /// forward`), when forwarding is on, opening the connection if it is not open. It runs at
    /// launch, on wake, when the panel opens and after an action, so a stack started from the
    /// app is reachable at once.
    func alignForwards() async {
        guard status?.forwards == true, status?.offline != true, let client else { return }
        _ = try? await client.output(["forward"])
        await readForwarding()
    }

    /// Runs the forwarder's watch while this Mac forwards anything, and stops the one it started
    /// when it does not. The watch follows the engine's session list over the connection the
    /// forwards are already on, which the engine sends only when a session starts or stops, so a
    /// forwarded session's ports follow it within seconds with nothing crossing the network in
    /// between; with the connection gone it opens nothing until alignForwards opens it again. A
    /// watch that has exited is started again at the next refresh. The long-running engineroom
    /// processes this app keeps while they are wanted: the forwarder, and the sweep.
    func keepWatches() {
        keepFeed()
        keepForwarder()
        keepSweeper()
    }

    func stopWatches() {
        stopFeed()
        stopForwarder()
        stopSweeper()
    }

    /// Keeps the feed open while the app runs (D-023): one connection to the engine that carries
    /// nothing until something changes, in place of a new connection for each look. It is started
    /// again where the sessions now are when this Mac switches to or from working local, and, after
    /// it ends, once its wait is over (feedEnded).
    func keepFeed() {
        guard let client, let status else { return }
        let offline = status.offline == true
        if let f = feed, f.isRunning, feedOffline == offline { return }
        if let f = feed, f.isRunning { f.stop() }
        feed = nil
        feedLive = false
        if let at = feedRetryAt, Date() < at { return }
        feedRetryAt = nil
        feedGeneration += 1
        let generation = feedGeneration
        feedOffline = offline
        feed = try? client.follow(
            onLine: { [weak self] line in self?.feedSaid(line, generation: generation) },
            onEnd: { [weak self] in self?.feedEnded(generation: generation) })
    }

    func stopFeed() {
        feedGeneration += 1
        feed?.stop()
        feed = nil
        feedLive = false
    }

    /// One line from the feed: the sessions as they now are, or why they could not be read. The
    /// open project's page is read again when the sessions changed, so it shows what is running
    /// without a timer of its own.
    private func feedSaid(_ line: Feed.Line, generation: Int) {
        guard generation == feedGeneration else { return }
        switch line {
        case .sessions(let reply):
            let changed = reply.sessions != sessions
            sessions = reply.sessions
            checkouts = reply.checkouts ?? [:]
            sessionsRead = reply.read
            problem = nil
            engineDown = false
            feedLive = true
            feedFailures = 0
            if changed, windowVisible, case .project(let name) = windowItem { Task { await loadProject(name) } }
        case .problem(let e):
            problem = e.error
            engineDown = e.engineDown ?? false
            if engineDown { sessions = [] }
        case .quiet:
            break
        }
    }

    /// The feed ended: the connection went (the Mac slept, the network moved), the engine did not
    /// answer, or the engine is too old to have one. It is started again after a wait: two seconds
    /// after one that ran, and longer each time one ends at once, up to five minutes. Until then
    /// the sessions are read when the panel opens, as they were before the feed.
    private func feedEnded(generation: Int) {
        guard generation == feedGeneration else { return }
        let ranFor = feed.map { Date().timeIntervalSince($0.began) } ?? 0
        feed = nil
        feedLive = false
        if ranFor < 5 {
            feedFailures += 1
        } else {
            feedFailures = 0
        }
        let wait = feedFailures == 0 ? 2 : min(300, 5 * pow(3, Double(feedFailures - 1)))
        feedRetryAt = Date().addingTimeInterval(wait)
        Task { [weak self] in
            try? await Task.sleep(nanoseconds: UInt64(wait * 1_000_000_000))
            guard let self, generation == self.feedGeneration else { return }
            self.keepFeed()
        }
    }

    /// The sweep (D-020): stops what outlived its session and forgets ended
    /// sessions. It runs with this app, never as a job of its own: on the engine for as long as the
    /// app runs, and on a client only while it works local, when the sessions are its own. One
    /// `engineroom sweep --watch` sweeps every minute in its own loop, so no shell starts each
    /// minute, and it ends by itself if this app ends without stopping it.
    func keepSweeper() {
        let want = status?.sweepsHere == true
        if want, sweeper?.isRunning != true, let client {
            sweeper = try? client.spawn(["sweep", "--watch"])
        } else if !want, let s = sweeper {
            if s.isRunning { s.terminate() }
            sweeper = nil
        }
    }

    func stopSweeper() {
        if let s = sweeper, s.isRunning { s.terminate() }
        sweeper = nil
    }

    func keepForwarder() {
        let want = status?.forwards == true && status?.offline != true
        if want, watcher?.isRunning != true, let client {
            watcher = try? client.spawn(["forward", "--watch"])
        } else if !want, let w = watcher {
            if w.isRunning { w.terminate() }
            watcher = nil
        }
    }

    /// Stops the watch this app started, when the app quits. The forwards themselves stay until
    /// forwarding is turned off: they belong to the connection, not to the app.
    func stopForwarder() {
        if let w = watcher, w.isRunning { w.terminate() }
        watcher = nil
    }

    /// Opening at login is macOS's own registration of this app (APP-LOGIN-001).
    var opensAtLogin: Bool {
        get { SMAppService.mainApp.status == .enabled }
        set {
            do {
                if newValue { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
            } catch {
                problem = "Opening at login could not be changed: \(error.localizedDescription)"
            }
            objectWillChange.send()
        }
    }

    // MARK: copying

    /// Copies to the clipboard; here the app does the copying itself, so it can say it copied.
    func copy(_ text: String, label: String) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        copied = label
    }

    /// The categories already in use, offered when choosing one.
    var categories: [String] {
        var seen = Set<String>()
        for l in repos?.roots ?? [] { for r in l.repos { if let c = r.category, !c.isEmpty { seen.insert(c) } } }
        for p in repos?.projects ?? [] { if let g = p.group, !g.isEmpty { seen.insert(g) } }
        return seen.sorted { $0.lowercased() < $1.lowercased() }
    }
}
