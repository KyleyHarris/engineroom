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

// MARK: the panel from the keyboard (D-033)

extension PanelProject.Card {
    /// Whether this is the project's main clone: as the feed says it, or, for a running session
    /// the feed sent no checkout for, when the session carries the project's own name.
    public func isMain(of project: String) -> Bool {
        facts?.main ?? (session?.name == project)
    }

    /// The `engineroom api` action that starts this checkout's session, without the way to start
    /// it (`--mode`), which is this Mac's setting: the main clone's by project, a worktree's by its
    /// work name, which opens the worktree that is already there. The card's Start button and the
    /// Return key both send this, so they cannot come to start different things.
    public func startAction(project: String) -> [String] {
        if isMain(of: project) { return ["start", project] }
        return ["new", project, facts?.work ?? facts?.role ?? facts?.session ?? session?.name ?? ""]
    }
}

extension PanelProject {
    /// What the Return key does for the project the panel shows.
    public enum KeyAction: Equatable, Sendable {
        /// Join this running session, as its card's Join does.
        case join(Session)
        /// Start a session with this action, as the card's Start does.
        case start([String])
    }

    /// The project the panel shows on its right: the one chosen, or the first when none was chosen
    /// or the chosen one is no longer listed. Nil only when the list is empty.
    public static func shown(_ projects: [PanelProject], chosen: String?) -> PanelProject? {
        projects.first { $0.id == chosen } ?? projects.first
    }

    /// The project an arrow key moves to from the one shown: the next down the list (`by` 1) or
    /// the one above (`by` -1). The list is the one the panel draws, in its order, so the keys walk
    /// the rows as they are seen. It stops at each end rather than wrapping round: holding the key
    /// reaches the first or the last row and stays there. Nil when the list is empty.
    public static func step(_ projects: [PanelProject], from chosen: String?, by delta: Int) -> String? {
        guard !projects.isEmpty else { return nil }
        // The row shown when nothing was chosen, or when what was chosen has gone, is the first.
        let at = projects.firstIndex { $0.id == chosen } ?? 0
        return projects[min(max(at + delta, 0), projects.count - 1)].id
    }

    /// The card Return acts on, so the panel can mark it: the first, in the order the cards are
    /// drawn, whose session waits for the developer (the reason the row carries Claude's mark);
    /// with none waiting, the first whose session runs; with none running, the first card, which
    /// is the main clone when the feed sent it. Nil for a project with no cards.
    public var keyCard: Card? {
        cards.first { $0.session?.isWaiting == true } ?? cards.first { $0.session != nil } ?? cards.first
    }

    /// What Return does for this project: joins the session of its key card when that runs, and
    /// otherwise starts that card's session. A project with no cards (pinned, nothing running, its
    /// checkouts not sent yet) is started by its name, as the detail's own Start does.
    public var keyAction: KeyAction {
        guard let card = keyCard else { return .start(["start", project]) }
        if let s = card.session { return .join(s) }
        return .start(card.startAction(project: project))
    }
}

/// A key the panel and the window answer themselves (D-033).
public enum PanelKey: Equatable, Sendable {
    case up, down, enter, escape

    /// From a key event's code: the two arrows, Return and the keypad's Enter, and Escape. Nil for
    /// every other key, which is never the panel's.
    public init?(keyCode: UInt16) {
        switch keyCode {
        case 126: self = .up
        case 125: self = .down
        case 36, 76: self = .enter
        case 53: self = .escape
        default: return nil
        }
    }

    /// What is on screen where the key was pressed.
    public enum Screen: Equatable, Sendable {
        case list        // the panel's list of projects
        case engineDown  // the panel saying the engine did not answer
        case action      // an action's page, in the panel or the window
        case task        // a page of the window reached from another: getting or registering a repository
        case other       // any other page of the window
    }

    /// Where the key was pressed and what was going on there.
    public struct Place: Equatable, Sendable {
        public var inPanel: Bool        // in the panel; false is the main window
        public var screen: Screen
        public var modified: Bool       // held with Command, Option, Control or Shift
        public var dialog: Bool         // a dialog is up over the window
        public var typing: Bool         // a field is being typed into
        public var composing: Bool      // an input method is part way through a character
        public var repeated: Bool       // the key is held down and this is one of its repeats

        public init(inPanel: Bool, screen: Screen, modified: Bool = false, dialog: Bool = false,
                    typing: Bool = false, composing: Bool = false, repeated: Bool = false) {
            self.inPanel = inPanel
            self.screen = screen
            self.modified = modified
            self.dialog = dialog
            self.typing = typing
            self.composing = composing
            self.repeated = repeated
        }
    }

    /// What the key does.
    public enum Effect: Equatable, Sendable {
        case pass        // not the panel's: the key goes on to the window untouched
        case taken       // the panel's, with nothing to do
        case back        // leave the page, as its link back does
        case closePanel  // close the panel, as clicking elsewhere does
        case step(Int)   // choose the project this many rows down the list
        case act         // join or start the chosen project (PanelProject.keyAction)
    }

    /// What this key does when pressed at a place.
    ///
    /// In the panel's list, Down and Up choose the next and the previous project, Return joins or
    /// starts the chosen one, and Escape closes the panel. On a page reached from another, in the
    /// panel or the window, Escape is the link back, as it cancels a dialog: from one of the
    /// page's fields too. Every other key at every other place is left alone, and so is:
    ///
    /// - a key held with Command, Option, Control or Shift, which belongs to a shortcut;
    /// - any key while a dialog is up, whose buttons answer Return and Escape;
    /// - Escape while an input method is composing a character, which it cancels;
    /// - in the panel's list, any key while a field is typed into: they are the field's.
    ///
    /// A held key repeats. The arrows walk the list for as long as they are held; Return asks
    /// once, and Escape goes back one step, not through a page and on out of the panel.
    public func effect(at place: Place) -> Effect {
        if place.modified || place.dialog { return .pass }
        if self == .escape, place.composing { return .pass }
        guard place.inPanel else {
            // The window answers only Escape, and only on a page that has a way back.
            guard self == .escape, place.screen == .action || place.screen == .task else { return .pass }
            return .back
        }
        if place.screen == .action {
            guard self == .escape else { return .pass }
            return place.repeated ? .taken : .back
        }
        if place.typing { return .pass }
        if self == .escape { return place.repeated ? .taken : .closePanel }
        // The engine did not answer: there is no list to walk and nothing to join.
        guard place.screen == .list else { return .pass }
        switch self {
        case .down: return .step(1)
        case .up: return .step(-1)
        case .enter: return place.repeated ? .taken : .act
        case .escape: return .pass
        }
    }
}
