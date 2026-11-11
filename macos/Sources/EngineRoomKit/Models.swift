import Foundation

// The shapes of `engineroom api` answers (cmd/engineroom/api.go). Every field the program may leave out is
// optional, so an answer from an older or newer engineroom still decodes; nothing here is computed,
// so what the window shows is what the program said.

public struct Terminal: Decodable, Sendable, Identifiable, Hashable {
    public let name: String
    public let bundle: String
    public let path: String?
    public let note: String?
    public var id: String { name }
    /// Only installed terminals are offered (APP-TERM-001).
    public var installed: Bool { path != nil }
}

public struct Status: Decodable, Sendable {
    public let role: String
    public let engine: String?
    public let offline: Bool
    public let `where`: String
    public let version: String?
    public let terminals: [Terminal]
    public let terminal: String?
    public let terminalProblem: String?
    public let start: String
    /// On a client: whether every running session's ports are kept reachable on this Mac (D-013).
    public let forward: Bool?
    /// On a client: whether any session's ports are, every session's or the ones switched on.
    public let forwarding: Bool?
    /// On the engine: when it was last released to GitHub for a client to take (information only).
    public let released: Date?
    public var isClient: Bool { role == "client" }
    /// Whether this machine's sessions are its own to sweep (D-020): the engine's always, a client's
    /// only while it works local. The app runs the sweep only while this holds.
    public var sweepsHere: Bool { role == "engine" || offline }
    /// Whether this Mac should be running the forwarder now: a client, working on the engine,
    /// forwarding every session or some.
    public var forwards: Bool { isClient && !offline && (forwarding ?? forward) == true }
}

public struct Session: Decodable, Sendable, Identifiable, Hashable {
    public let name: String
    public let project: String?
    public let checkout: String?
    public let path: String?
    public let ended: Bool
    public let waiting: String?
    public let question: String?
    public let created: String?
    /// On a client working on the engine: a session on this Mac, started while it was offline and
    /// still running, listed with the engine's (CONN-HANDOFF-006). Its actions are done on this Mac.
    public let here: Bool?
    /// The slot the session's worktree runs on and that slot's ports (D-013), when its project
    /// has ports.
    public let slot: Int?
    public let ports: [Port]?
    /// The addresses the session's stack serves, to open from the app (a terminal over SSH
    /// cannot copy them).
    public let links: [Link]?
    /// A checkout's Ask Claude session (<session>-ai, D-018): opened or shut down, never
    /// navigated to as a project.
    public let assistant: Bool?
    /// Its checkout's branch on GitHub, only once GitHub has the branch, for the panel's GitHub
    /// line (APP-LIST-003); the repository's own links are in the repositories list.
    public let github: BranchLinks?
    public var isAssistant: Bool { assistant == true }
    /// The engine and this Mac can each have a session of the same name, so the name alone is not
    /// an identity.
    public var id: String { isHere ? "here:" + name : name }
    public var isWaiting: Bool { waiting == "waiting" }
    public var isHere: Bool { here == true }
    /// The arguments an action on this session takes: its name, and --here for one on this Mac.
    public var target: [String] { isHere ? [name, "--here"] : [name] }
}

/// One named port of a session's slot.
public struct Port: Decodable, Sendable, Hashable {
    public let name: String
    public let port: Int
}

/// One address a checkout's stack serves.
public struct Link: Decodable, Sendable, Hashable {
    public let name: String
    public let url: String
}

/// One lifecycle script of a project and where it comes from; `from` is nil when it has none.
public struct ScriptView: Decodable, Sendable, Hashable {
    public let name: String
    public let from: String?
    public let path: String?
}

/// One of a project's named tasks.
public struct TaskView: Decodable, Sendable, Hashable {
    public let name: String
    public let about: String?
}

/// One secret file and how it compares with its saved copy.
public struct SecretView: Decodable, Sendable, Hashable {
    public let name: String
    public let state: String
}

/// One checkout of a project: the main clone or a worktree, with its session (which it has or
/// would have, named by `session`), its slot, ports and links.
public struct CheckoutView: Decodable, Sendable, Hashable, Identifiable {
    public let session: String
    public let path: String
    public let checkout: String
    public let branch: String?
    public let main: Bool
    public let work: String?
    public let running: Bool
    public let ended: Bool
    public let waiting: String?
    public let question: String?
    public let changes: Int
    public let slot: Int?
    public let ports: [Port]?
    public let links: [Link]?
    /// Set when this checkout's folder shares its name with another checkout's, so it would share
    /// that one's session: its session and stack actions are not offered, and this says why.
    public let clash: String?
    /// The checkout's branch on GitHub, only once GitHub has the branch (APP-PAGE-006).
    public let github: BranchLinks?
    /// Set for a worktree kept for a role (D-024): its role, which titles its card; it moves on
    /// to its next branch rather than being closed when one is done.
    public let role: String?
    public var id: String { path }
    /// Only a running session counts: one that ended is not there (D-015), and Start starts it
    /// fresh.
    public var hasSession: Bool { running }
    public var isWaiting: Bool { waiting == "waiting" }
}

/// A repository's pages on GitHub: its code, issues and pull requests.
public struct GitHubLinks: Decodable, Sendable, Hashable {
    /// owner/name, as GitHub writes it.
    public let repo: String
    public let code: String
    public let issues: String
    public let pulls: String
}

/// A branch's pages on GitHub: its code, and the pull requests from it (none for the default
/// branch, which pull requests go into).
public struct BranchLinks: Decodable, Sendable, Hashable {
    public let code: String
    public let pulls: String?
}

/// One project's page (`engineroom api project <name>`).
public struct ProjectPage: Decodable, Sendable, Hashable {
    public let name: String
    public let path: String
    public let checkout: String
    public let category: String?
    public let pinned: Bool?
    public let identity: String?
    /// The repository's pages on GitHub, when its origin is there (APP-PAGE-005).
    public let github: GitHubLinks?
    public let settings: String
    public let reserve: [String]?
    public let slots: Int
    public let scripts: [ScriptView]
    public let secrets: [SecretView]?
    /// The project's own named tasks (engineroom.toml's [[task]]), run in a checkout.
    public let tasks: [TaskView]?
    public let checkouts: [CheckoutView]
    public let problem: String?
    /// Servers on the project's ports that belong to no running session (D-020): shown, each with
    /// a Stop for that one, never stopped for the developer.
    public let loose: [LooseServer]?
    public func has(_ script: String) -> Bool { scripts.first { $0.name == script }?.from != nil }
}

/// One server on a project's port with no running session to belong to.
public struct LooseServer: Decodable, Sendable, Hashable, Identifiable {
    public let pid: Int
    public let ports: [Int]
    public let command: String
    public let started: String
    public let folder: String?
    /// The ended session it was started for, when its environment names one.
    public let session: String?
    public var id: Int { pid }
}

/// What the forwarder on a client last found and did (`engineroom forward status --json`).
public struct ForwardStatus: Decodable, Sendable {
    public struct Last: Decodable, Sendable {
        public let at: Date?
        public let ok: Bool?
        public let error: String?
        public let notes: [String]?
        public let sessions: Int?
        public let ports: Int?
    }
    public let enabled: Bool
    public let connected: Bool
    public let forwarded: [String: String]
    public let last: Last?
    public let skip: [String]?
    /// The sessions switched on one by one, while forwarding every session is off.
    public let sessions: [String]?

    /// Whether a session's ports are to be forwarded here: with every session forwarded, unless it
    /// is left out; otherwise, if it was switched on.
    public func wants(_ session: String) -> Bool {
        enabled ? !(skip ?? []).contains(session) : (sessions ?? []).contains(session)
    }
}

public struct SessionsReply: Decodable, Sendable {
    public let sessions: [Session]
    public let read: String?
    public let `where`: String?
    /// Each checkout of the pinned projects and of those with a running session, by project, as
    /// the feed sends them (D-025); absent from a plain read of the sessions.
    public let checkouts: [String: [CheckoutFacts]]?
}

/// What the panel's compact card says about one checkout, running or not (D-025): which it is,
/// the branch it holds, and what in it is unsaved, unpushed, behind the remote's main, or merged.
/// Read on the engine from git as it knows it now; "behind" and "merged" are as of the last fetch.
public struct CheckoutFacts: Decodable, Sendable, Hashable {
    public let session: String
    public let path: String
    public let main: Bool
    public let work: String?
    public let role: String?
    public let branch: String?
    public let changes: Int
    public let unpushed: Int
    public let behind: Int
    public let merged: Bool?
    /// Its own ports and the addresses its stack serves, running or not.
    public let slot: Int?
    public let ports: [Port]?
    public let links: [Link]?
    /// The project has an up or a down script: the card offers Run app and Stop app.
    public let up: Bool?
    public let down: Bool?
    public var isMerged: Bool { merged == true }
}

public struct Root: Decodable, Sendable, Hashable {
    public let name: String
    public let path: String
    public let owner: String
    public let account: String?
}

public struct Problem: Decodable, Sendable, Hashable {
    public let reason: String
    public let fix: String?
}

public struct Repo: Decodable, Sendable, Identifiable, Hashable {
    public let name: String
    public let description: String?
    public let visibility: String?
    public let archived: Bool?
    /// "registered", "folder" (here, not registered), or nil (on GitHub, not here).
    public let here: String?
    public let project: String?
    public let category: String?
    public let pinned: Bool?
    public let local: Bool?
    /// A repository on GitHub, not here, that the developer hid from the list.
    public let hidden: Bool?
    public var id: String { name }
    public var isRegistered: Bool { here == "registered" }
    public var isUnregisteredFolder: Bool { here == "folder" }
    public var isOnGitHubOnly: Bool { here == nil }
}

public struct Listing: Decodable, Sendable, Hashable {
    public let root: Root
    public let read: String?
    public let repos: [Repo]
    public let problem: Problem?

    /// The repositories grouped by category, categories in alphabetical order and the ones with
    /// none last, as the terminal interface lists projects. Inside a category the repositories
    /// are in alphabetical order ignoring case, sorted here too so an engine older than that order
    /// is listed the same way.
    public var byCategory: [(String, [Repo])] {
        var groups: [String: [Repo]] = [:]
        for r in repos { groups[r.category ?? "", default: []].append(r) }
        let named = groups.keys.filter { !$0.isEmpty }.sorted { $0.lowercased() < $1.lowercased() }
        var out = named.map { ($0, Listing.byName(groups[$0]!)) }
        if let none = groups[""] { out.append(("", Listing.byName(none))) }
        return out
    }

    /// Repositories by name, ignoring case; names that differ only in case keep a fixed order.
    static func byName(_ repos: [Repo]) -> [Repo] {
        repos.sorted {
            let a = $0.name.lowercased(), b = $1.name.lowercased()
            return a != b ? a < b : $0.name < $1.name
        }
    }
}

public struct ProjectView: Decodable, Sendable, Identifiable, Hashable {
    public let name: String
    public let path: String?
    public let group: String?
    public let pinned: Bool?
    public var id: String { name }
}

public struct ReposReply: Decodable, Sendable {
    public let roots: [Listing]
    public let projects: [ProjectView]
    /// Each registered project's repository on GitHub, by project name; one not on GitHub is not
    /// here (APP-LIST-003).
    public let github: [String: GitHubLinks]?
}

public struct Command: Decodable, Sendable, Hashable {
    public let argv: [String]
    public let dir: String?
    public let why: String?
    public let onFail: String?
}

public struct Plan: Decodable, Sendable, Hashable {
    public let title: String
    public let machine: String?
    public let commands: [Command]
    /// What the action leaves alone and why, said before its commands (a handoff's notes,
    /// CONN-HANDOFF-003).
    public let notes: [String]?
    /// Other actions the developer may want because of what the plan found, each run only if
    /// chosen (a release's "commit as partial work").
    public let offers: [Offer]?
    /// A plan that cannot run as asked: no commands, and its notes say what is in the way (a
    /// handover that cannot put the other Mac in this one's state).
    public let blocked: Bool?
}

/// An action a plan suggests: its label, and the `engineroom api` action that does it.
public struct Offer: Decodable, Sendable, Hashable {
    public let label: String
    public let action: [String]
}

public struct PlanReply: Decodable, Sendable {
    public let plan: Plan
    public let lines: [String]
}

/// One line of `engineroom api run`: the plan, a step as it changes, or the outcome.
/// How far reading one machine has got while a plan is made (a handover reads both Macs first):
/// the machine as the plan names it, the projects read of how many, the last one, and the command
/// doing the reading. Total is 0 until the machine has said how many it reads.
public struct PlanProgress: Decodable, Sendable, Equatable {
    public let side: String
    public let done: Int
    public let total: Int
    public let project: String?
    public let command: String?

    public init(side: String, done: Int, total: Int, project: String? = nil, command: String? = nil) {
        self.side = side
        self.done = done
        self.total = total
        self.project = project
        self.command = command
    }

    /// One `{"progress": ...}` line from engineroom, or nil for any other line.
    public static func parse(_ line: Data) -> PlanProgress? {
        struct Line: Decodable { let progress: PlanProgress? }
        return (try? JSONDecoder().decode(Line.self, from: line))?.progress
    }
}

public struct RunEvent: Decodable, Sendable {
    public let event: String
    /// With event "progress": how far reading a machine has got, before the plan arrives.
    public let progress: PlanProgress?
    public let plan: Plan?
    public let lines: [String]?
    public let index: Int?
    public let status: String?
    public let exit: Int?
    public let output: String?
    public let onFail: String?
    public let ok: Bool?
    public let error: String?
    public let history: String?
}

public struct HistoryCommand: Decodable, Sendable, Hashable {
    public let display: String
    public let why: String?
    public let status: String
    public let exit: Int
    public let output: String?
}

public struct HistoryEntry: Decodable, Sendable, Identifiable, Hashable {
    public let id: String
    public let started: Date
    public let machine: String
    public let title: String
    public let status: String
    /// What the plan said besides its commands, as it ran: a handover's carried and set-aside
    /// work, and what it left alone.
    public let notes: [String]?
    public let commands: [HistoryCommand]
}

/// A refusal or failure from `engineroom api` (exit 3), with whether the engine did not answer.
public struct APIError: Error, Decodable, Sendable, LocalizedError {
    public let error: String
    public let engineDown: Bool?
    public let problem: Problem?
    /// What would make a refused action safe (D-024): Close and Next branch, refused because work
    /// would be lost, offer to save it to the branch, put it aside or push it.
    public let offers: [Offer]?
    public var errorDescription: String? { error }
    public init(_ message: String, engineDown: Bool = false) {
        self.error = message
        self.engineDown = engineDown
        self.problem = nil
        self.offers = nil
    }
}

/// Copying all of an action's commands joins them so a paste stops at the first failure, as the
/// terminal interface does (LEARN-COPY-002).
public func joinAll(_ lines: [String]) -> String {
    lines.joined(separator: " &&\n") + "\n"
}

/// `engineroom api engine-version`: this Mac's build, the engine's, and which of them is behind,
/// decided by engineroom from git's ancestry ("none", "here", "engine", "apart" or "unknown"). An
/// engineroom too old to say leaves `behind` out, which is read as not knowing.
public struct EngineVersion: Decodable, Equatable {
    public let here: String
    public let engine: String
    public let behind: String?
    /// The engine's own machine name (CONN-ROLE-003), shown in place of the SSH name this Mac reaches
    /// it by. Nil from an engine too old to give one, or when it could not be read.
    public let name: String?

    public init(here: String, engine: String, behind: String?, name: String? = nil) {
        self.here = here
        self.engine = engine
        self.behind = behind
        self.name = name
    }

    /// What the banner says: it tells the machine that is behind to update, and only that one (a
    /// client newer than its engine was once told to update itself). When git cannot say which is
    /// older it names both and tells neither. Nil when they match.
    public var advice: String? { EngineVersion.advice(here: here, engine: engine, behind: behind) }

    public static func advice(here: String, engine: String, behind: String?) -> String? {
        let update = "git pull, then scripts/install.sh --app"
        switch behind {
        case "none": return nil
        case "here": return "The engine runs engineroom \(engine), newer than this Mac's \(here). Update this Mac: \(update)."
        case "engine": return "This Mac runs engineroom \(here), newer than the engine's \(engine). Update the engine: \(update), on the engine."
        case "apart": return "The engine runs engineroom \(engine) and this Mac runs \(here), built from different branches. Put both on the same branch, then \(update) on each."
        default: return here == engine ? nil : "The engine runs engineroom \(engine) and this Mac runs \(here)."
        }
    }
}

/// The engine's health, as `engineroom health --json` reports it (acceptance/health, D-037): its
/// setup items, staying awake, and each running session's Remote Control and app, each present or
/// missing with how it was checked and, when missing, its fix. On a client it is the engine's
/// report, asked of the engine; an engine not reached is said in `unreached`, with no items.
public struct HealthReport: Decodable, Sendable {
    /// Where the items were checked: "this machine", or the engine's SSH name from a client.
    public let machine: String
    /// The engine not reached from a client, with its cause; nothing was checked then.
    public let unreached: String?
    public let items: [HealthItem]
    public let missing: Int

    private enum CodingKeys: String, CodingKey { case machine, unreached, items, missing }

    /// An engine not reached has no items, which engineroom writes as null rather than an empty list.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        machine = try c.decode(String.self, forKey: .machine)
        unreached = try c.decodeIfPresent(String.self, forKey: .unreached)
        items = try c.decodeIfPresent([HealthItem].self, forKey: .items) ?? []
        missing = try c.decode(Int.self, forKey: .missing)
    }

    /// Whether nothing is missing and the engine was reached, as the command's exit status says.
    public var ready: Bool { missing == 0 && (unreached ?? "").isEmpty }

    /// The items under their headings, in the report's order: the setup items, staying awake, then
    /// each running session's items under the session's name, as `engineroom health` prints them.
    public var sections: [HealthSection] {
        var out: [HealthSection] = []
        for item in items {
            let title: String
            switch item.group {
            case "setup": title = "Setup"
            case "awake": title = "Staying awake"
            default: title = item.session ?? "Running sessions"
            }
            if out.last?.title == title { out[out.count - 1].items.append(item) }
            else { out.append(HealthSection(title: title, isSession: item.group == "session", items: [item])) }
        }
        return out
    }
}

/// One heading of the health report and its items.
public struct HealthSection: Sendable {
    public let title: String
    /// Whether the heading is a running session's name, listed under Running sessions.
    public let isSession: Bool
    public var items: [HealthItem]
}

/// One thing the engine needs to be worked on from away.
public struct HealthItem: Decodable, Sendable, Hashable {
    public let name: String
    public let present: Bool
    /// The command run, or the file read, written so a person can do the same by hand.
    public let checked: String
    /// What the check saw.
    public let found: String
    /// What would make it present; nil when present.
    public let fix: String?
    /// setup, awake or session.
    public let group: String
    public let session: String?
    /// The `engineroom api` action that applies the fix, when the program can (HEALTH-FIX-001); nil
    /// for a fix the developer runs themselves (HEALTH-FIX-002).
    public let action: [String]?

    /// The action the page may offer for this item's fix, or nil when it offers none. A present
    /// item has nothing to fix. A client working local does its actions on this Mac, while the
    /// report is the engine's: the fix would start the app here, not where it was found missing,
    /// so it is shown as words only until the work is handed back.
    public func runnableFix(workingLocal: Bool) -> [String]? {
        guard !present, !workingLocal, let a = action, !a.isEmpty else { return nil }
        return a
    }
}
