import Foundation

/// What the menu bar panel lists, and in what order (APP-LIST-001, APP-LIST-002).
///
/// The order never depends on whether Claude is waiting. A waiting session is marked on its own
/// row, where it always is, so a row is found where it was last time and the panel does not move
/// under the pointer when a session starts or stops waiting.
public struct PanelList: Equatable, Sendable {
    /// One pinned project and its running sessions, main clone and worktrees together; with none,
    /// the panel offers to start it.
    public struct Pinned: Equatable, Sendable {
        public let project: String
        public let sessions: [Session]
    }

    /// The pinned projects, first, by name; each always listed, running or not.
    public let pinned: [Pinned]
    /// The engine's other sessions, by name.
    public let running: [Session]
    /// Ask Claude sessions, after the rest, by name.
    public let helpers: [Session]
    /// This Mac's own sessions, started while it was offline (CONN-HANDOFF-006), by name.
    public let here: [Session]

    /// Lays the sessions out for the panel. `pinned` is the pinned projects' names. A session
    /// that ended is not listed (D-015).
    public init(sessions: [Session], pinned: [String]) {
        let live = sessions.filter { !$0.ended }
        let engine = PanelList.byName(live.filter { !$0.isHere })
        let pinnedNames = PanelList.byName(Array(Set(pinned)))
        let pinnedSet = Set(pinnedNames)
        self.pinned = pinnedNames.map { name in
            Pinned(project: name, sessions: engine.filter { $0.project == name && !$0.isAssistant })
        }
        running = engine.filter { !$0.isAssistant && !pinnedSet.contains($0.project ?? "") }
        helpers = engine.filter(\.isAssistant)
        here = PanelList.byName(live.filter(\.isHere))
    }

    /// Whether Claude is waiting for the developer in any running session: the menu bar icon then
    /// carries Claude's mark, panel open or not (APP-FEED-005). An ended session waits for nobody.
    public static func anyWaiting(_ sessions: [Session]) -> Bool {
        sessions.contains { !$0.ended && $0.isWaiting }
    }

    /// Names in alphabetical order ignoring case; names that differ only in case keep a fixed
    /// order. A worktree's session (`<repo>-<work>`) follows its main clone's (`<repo>`).
    static func byName(_ names: [String]) -> [String] {
        names.sorted { a, b in
            let la = a.lowercased(), lb = b.lowercased()
            return la != lb ? la < lb : a < b
        }
    }

    static func byName(_ sessions: [Session]) -> [Session] {
        sessions.sorted { a, b in
            let la = a.name.lowercased(), lb = b.name.lowercased()
            return la != lb ? la < lb : a.name < b.name
        }
    }
}

/// What one checkout's light says, on the panel's list and on its card.
public enum PanelLight: Equatable, Sendable {
    case waiting, running, idle

    /// The light of one project wherever it is listed outside the panel (the window's sidebar,
    /// the repositories list), from the feed's sessions: Claude's mark when any of its running
    /// sessions waits for the developer, running when one runs, off otherwise. Every session of
    /// the project counts, an Ask Claude one and one on this Mac included, since each of these
    /// lists has one line for the whole project. A session that ended is not running and waits
    /// for nobody (D-015).
    public static func of(project: String, in sessions: [Session]) -> PanelLight {
        let live = sessions.filter { $0.project == project && !$0.ended }
        if live.contains(where: \.isWaiting) { return .waiting }
        return live.isEmpty ? .idle : .running
    }
}

/// One row of the panel's list and what its detail shows (D-025): a project, its light for all its
/// sessions together, and one card per checkout, running or not.
public struct PanelProject: Identifiable, Equatable, Sendable {
    /// One card: a checkout as the feed describes it, its running session if it has one, or a
    /// running session the feed has no checkout for (an engine too old to send them, or a session in a
    /// folder no project knows).
    public struct Card: Identifiable, Equatable, Sendable {
        public let facts: CheckoutFacts?
        public let session: Session?
        public var id: String { facts?.session ?? session?.name ?? "" }
        public var light: PanelLight {
            guard let session else { return .idle }
            return session.isWaiting ? .waiting : .running
        }
    }

    public let project: String
    public let pinned: Bool
    /// The project's running sessions other than Ask Claude ones, by name.
    public let sessions: [Session]
    /// Its Ask Claude sessions, shown as one line each in the detail.
    public let helpers: [Session]
    /// Main clone first, then each worktree by session name.
    public let cards: [Card]
    /// Listed only because the developer was last at it (keep, in list): not pinned and nothing
    /// running, so the panel opens on what they last did, its Start in reach.
    public var recent: Bool = false
    public var id: String { project }

    /// Claude's mark when any of its sessions waits, running when one runs, off otherwise.
    public var light: PanelLight {
        if sessions.contains(where: \.isWaiting) { return .waiting }
        return sessions.isEmpty ? .idle : .running
    }

    /// The panel's projects in its one fixed order (D-022): the pinned ones by name, each always
    /// listed, then every other project with a running session, by name. A running session with no
    /// project is listed as a project of its own name. Waiting never changes the order. keep is the
    /// project the developer last chose: when it is in neither group (they stopped its last
    /// session), it is listed last as recent, so the panel stays on it rather than jumping to
    /// another project.
    public static func list(_ list: PanelList, checkouts: [String: [CheckoutFacts]], keep: String? = nil) -> [PanelProject] {
        var out = list.pinned.map { p in
            make(p.project, pinned: true, sessions: p.sessions, helpers: list.helpers, checkouts: checkouts)
        }
        var others: [String: [Session]] = [:]
        for s in list.running { others[(s.project ?? "").isEmpty ? s.name : s.project!, default: []].append(s) }
        for name in PanelList.byName(Array(others.keys)) {
            out.append(make(name, pinned: false, sessions: others[name]!, helpers: list.helpers, checkouts: checkouts))
        }
        if let keep, !keep.isEmpty, !out.contains(where: { $0.project == keep }) {
            var recent = make(keep, pinned: false, sessions: [], helpers: list.helpers, checkouts: checkouts)
            recent.recent = true
            out.append(recent)
        }
        return out
    }

    static func make(_ project: String, pinned: Bool, sessions: [Session], helpers: [Session], checkouts: [String: [CheckoutFacts]]) -> PanelProject {
        let facts = (checkouts[project] ?? []).sorted { a, b in a.main != b.main ? a.main : a.session < b.session }
        var cards = facts.map { f in Card(facts: f, session: sessions.first { $0.name == f.session }) }
        let known = Set(facts.map(\.session))
        cards += sessions.filter { !known.contains($0.name) }.map { Card(facts: nil, session: $0) }
        return PanelProject(project: project, pinned: pinned, sessions: sessions,
                            helpers: helpers.filter { $0.project == project }, cards: cards)
    }
}
