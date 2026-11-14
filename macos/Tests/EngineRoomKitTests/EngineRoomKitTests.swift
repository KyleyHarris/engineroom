import Foundation
import XCTest
@testable import EngineRoomKit

/// The app's side of `engineroom api`: answers decode, refusals arrive as the program worded them, a run
/// streams its events in order, and engineroom is found where it is installed. The program itself is
/// covered by the journeys; these check the app reads it faithfully.
final class EngineRoomKitTests: XCTestCase {
    /// A stand-in engineroom: prints the file named after its arguments and exits with the code in it.
    private func fakeEngineroom(_ answers: [String: (String, Int32)]) throws -> EngineRoomClient {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        var script = "#!/bin/sh\ncase \"$*\" in\n"
        for (args, (out, code)) in answers {
            let file = dir.appendingPathComponent(UUID().uuidString)
            try out.write(to: file, atomically: true, encoding: .utf8)
            script += "\"\(args)\") cat '\(file.path)'; exit \(code) ;;\n"
        }
        script += "*) echo unknown >&2; exit 2 ;;\nesac\n"
        let exe = dir.appendingPathComponent("engineroom")
        try script.write(to: exe, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: exe.path)
        return EngineRoomClient(executable: exe.path)
    }

    // An answer larger than a pipe's buffer (64 KB) comes back whole, and the call returns. Before,
    // the output was read only after engineroom exited, so engineroom blocked writing it and the call never
    // returned: the menu's refresh hung once the history passed 64 KB. Fails (times out) if the
    // output is read only at exit again, or comes back cut.
    func testALargeAnswerIsReadWhole() async throws {
        let big = String(repeating: "0123456789abcdef", count: 16 * 1024)
        let client = try fakeEngineroom(["history --json": (big, 0)])
        // Waited for with a deadline that does not wait on the call itself: a call that never
        // returns fails the test at the deadline instead of hanging it.
        let done = expectation(description: "the answer came back")
        let box = Collected()
        var code: Int32 = -1
        Task {
            if let (data, c) = try? await client.output(["history", "--json"]) {
                box.append(data)
                code = c
            }
            done.fulfill()
        }
        await fulfillment(of: [done], timeout: 10)
        let data = box.data
        XCTAssertEqual(code, 0)
        XCTAssertEqual(data.count, big.utf8.count)
    }

    // A real `engineroom api sessions` answer decodes, and the waiting session is recognised.
    func testSessionsDecode() async throws {
        let c = try fakeEngineroom(["api sessions": ("""
        {"read":"zellij list-sessions --no-formatting","sessions":[{"name":"project-b","project":"project-b","checkout":"~/src/project-b","path":"/x/project-b","ended":false,"waiting":"waiting","question":"Approve?","created":"3s ago"},{"name":"project-a","project":"","checkout":"","ended":true,"waiting":"","created":"1h"}],"where":"engine"}
        """, 0)])
        let s = try await c.sessions()
        XCTAssertEqual(s.sessions.map(\.name), ["project-b", "project-a"])
        XCTAssertTrue(s.sessions[0].isWaiting)
        XCTAssertEqual(s.sessions[0].question, "Approve?")
        XCTAssertFalse(s.sessions[1].isWaiting)
    }

    // D-013: a session's slot and ports decode, and the status says whether this Mac should run
    // the forwarder: only a client, working on the engine, with forwarding on. Fails if an engine, an
    // offline client, or a client with forwarding off would start one.
    func testPortsAndForwarding() async throws {
        let c = try fakeEngineroom([
            "api sessions": ("""
            {"sessions":[{"name":"app","ended":false,"slot":2,"ports":[{"name":"ui","port":20110},{"name":"api","port":20111}]}],"where":"engine"}
            """, 0),
            "api status": ("""
            {"role":"client","engine":"engine","offline":false,"where":"engine","terminals":[],"start":"zellij","forward":true}
            """, 0),
        ])
        let s = try await c.sessions()
        XCTAssertEqual(s.sessions[0].slot, 2)
        XCTAssertEqual(s.sessions[0].ports, [Port(name: "ui", port: 20110), Port(name: "api", port: 20111)])
        let st = try await c.status()
        XCTAssertTrue(st.forwards)
        let decode = { (json: String) throws -> Status in try JSONDecoder().decode(Status.self, from: Data(json.utf8)) }
        XCTAssertFalse(try decode(#"{"role":"engine","offline":false,"where":"engine","terminals":[],"start":"zellij","forward":true}"#).forwards)
        XCTAssertFalse(try decode(#"{"role":"client","offline":true,"where":"this machine","terminals":[],"start":"zellij","forward":true}"#).forwards)
        XCTAssertFalse(try decode(#"{"role":"client","offline":false,"where":"engine","terminals":[],"start":"zellij"}"#).forwards)
    }

    // D-020: the app runs the sweep on the engine, and on a client only while it
    // works local. Fails if a client working on the engine would sweep (a process every minute for
    // nothing) or the engine or a client working local would not.
    func testTheSweepRunsOnlyWhereTheSessionsAreThisMachines() throws {
        let decode = { (json: String) throws -> Status in try JSONDecoder().decode(Status.self, from: Data(json.utf8)) }
        XCTAssertTrue(try decode(#"{"role":"engine","offline":false,"where":"this machine","terminals":[],"start":"zellij"}"#).sweepsHere)
        XCTAssertTrue(try decode(#"{"role":"client","offline":true,"where":"this machine","terminals":[],"start":"zellij"}"#).sweepsHere)
        XCTAssertFalse(try decode(#"{"role":"client","offline":false,"where":"engine","terminals":[],"start":"zellij"}"#).sweepsHere)
    }

    // D-039: a Mac used on its own runs its own sessions as the engine does, so the app sweeps them
    // there, and it is named in the app's words, never as the engine. Fails if a local Mac would not
    // sweep (its stopped sessions' processes would outlive them) or is labelled as the engine.
    func testALocalMacSweepsItsOwnSessionsAndIsNamedAsItself() throws {
        let decode = { (json: String) throws -> Status in try JSONDecoder().decode(Status.self, from: Data(json.utf8)) }
        let local = try decode(#"{"role":"local","offline":false,"where":"this machine","terminals":[],"start":"zellij"}"#)
        XCTAssertTrue(local.sweepsHere)
        XCTAssertTrue(local.isLocal)
        XCTAssertFalse(local.isClient)
        XCTAssertEqual(local.roleLabel, "this Mac only")
        XCTAssertEqual(try decode(#"{"role":"engine","offline":false,"where":"this machine","terminals":[],"start":"zellij"}"#).roleLabel, "engine · this Mac")
        XCTAssertEqual(try decode(#"{"role":"","offline":false,"where":"this machine","terminals":[],"start":"zellij"}"#).roleLabel, "no role")
    }

    // CONN-HANDOFF-006: a session on this Mac listed with the engine's keeps its own identity when
    // the engine has one of the same name, and its actions carry --here so they are done on this
    // Mac. Fails if the two share an id or the flag is missing.
    func testHereSessionsAreTheirOwn() async throws {
        let c = try fakeEngineroom(["api sessions": ("""
        {"read":"x","sessions":[{"name":"project-a","ended":false,"waiting":""},{"name":"project-a","ended":false,"waiting":"","here":true}],"where":"engine engine"}
        """, 0)])
        let s = try await c.sessions().sessions
        XCTAssertNotEqual(s[0].id, s[1].id)
        XCTAssertEqual(s[0].target, ["project-a"])
        XCTAssertEqual(s[1].target, ["project-a", "--here"])
    }

    // CONN-HANDOFF-003: a plan's notes reach the window, and a plan with nothing to run decodes.
    func testPlanNotesDecode() async throws {
        let c = try fakeEngineroom(["api plan handoff to-engine": ("""
        {"plan":{"title":"Hand over from this machine to the engine","machine":"this machine and the engine","commands":[],"notes":["Nothing needs handing over."]},"lines":[]}
        """, 0)])
        let p = try await c.plan(["handoff", "to-engine"])
        XCTAssertEqual(p.plan.notes, ["Nothing needs handing over."])
        XCTAssertTrue(p.lines.isEmpty)
    }

    // CONN-HANDOFF-013: while a plan is made, each {"progress": ...} line engineroom writes to its
    // standard error reaches the app in order, and the plan on standard output is still read whole.
    // Fails if a step is lost, arrives out of order, or the progress spoils the plan.
    @MainActor
    func testPlanProgressReachesTheApp() async throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let exe = dir.appendingPathComponent("engineroom")
        try """
        #!/bin/sh
        echo '{"progress":{"side":"the engine","done":0,"total":2,"command":"ssh engine.example -- engineroom handoff state --json"}}' >&2
        echo '{"progress":{"side":"the engine","done":1,"total":2,"project":"project-a"}}' >&2
        echo '{"progress":{"side":"the engine","done":2,"total":2,"project":"project-b"}}' >&2
        echo '{"progress":{"side":"this machine","done":1,"total":1,"project":"project-a"}}' >&2
        echo '{"plan":{"title":"Hand over from the engine to this machine","commands":[],"notes":[]},"lines":[]}'
        """.write(to: exe, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: exe.path)
        var steps: [PlanProgress] = []
        let p = try await EngineRoomClient(executable: exe.path).plan(["handoff", "from-engine"]) { steps.append($0) }
        XCTAssertEqual(p.plan.title, "Hand over from the engine to this machine")
        // The steps are delivered on the main queue after the reply is read: let them land.
        for _ in 0..<50 where steps.count < 4 { try await Task.sleep(nanoseconds: 20_000_000) }
        XCTAssertEqual(steps.map(\.done), [0, 1, 2, 1])
        XCTAssertEqual(steps.map(\.side), ["the engine", "the engine", "the engine", "this machine"])
        XCTAssertEqual(steps.first?.command, "ssh engine.example -- engineroom handoff state --json")
        XCTAssertNil(PlanProgress.parse(Data(#"{"plan":{}}"#.utf8)))
    }

    // A refusal (exit 3) reaches the window in the program's own words, with engineDown kept, so the
    // window can offer retry or offline.
    func testRefusalIsTheProgramsWords() async throws {
        let c = try fakeEngineroom(["api sessions": ("{\"error\":\"the engine did not answer: timeout\",\"engineDown\":true}", 3)])
        do {
            _ = try await c.sessions()
            XCTFail("a refusal decoded as an answer")
        } catch let e as APIError {
            XCTAssertEqual(e.error, "the engine did not answer: timeout")
            XCTAssertEqual(e.engineDown, true)
        }
    }

    // The repositories answer decodes with its marks, and categories group as the terminal lists
    // them: alphabetical, ignoring case, uncategorised last.
    func testReposAndCategories() async throws {
        let c = try fakeEngineroom(["api repos": ("""
        {"roots":[{"root":{"name":"owners","path":"/r","owner":"owner-a","account":"account-a"},"read":"engineroom gh ...","repos":[{"name":"widget","visibility":"private"},{"name":"sprocket","here":"registered","project":"sprocket","category":"tools"},{"name":"scratch","here":"folder","local":true},{"name":"Tango"},{"name":"cog","here":"registered","project":"cog","category":"Apps"}]}],"projects":[]}
        """, 0)])
        let r = try await c.repos()
        let l = r.roots[0]
        XCTAssertEqual(l.byCategory.map(\.0), ["Apps", "tools", ""])
        // Inside a category, by name ignoring case, whatever order the engine sent: a sort that
        // minds case would put "Tango" first, and none would leave "widget" first.
        XCTAssertEqual(l.byCategory.last!.1.map(\.name), ["scratch", "Tango", "widget"])
        let by = Dictionary(uniqueKeysWithValues: l.repos.map { ($0.name, $0) })
        XCTAssertTrue(by["widget"]!.isOnGitHubOnly)
        XCTAssertTrue(by["sprocket"]!.isRegistered)
        XCTAssertTrue(by["scratch"]!.isUnregisteredFolder)
    }

    // A run's events arrive in the order printed, and its outcome is the done event's.
    @MainActor
    func testRunStreamsEventsInOrder() async throws {
        let events = """
        {"event":"plan","plan":{"title":"Start project-a","machine":"engine","commands":[{"argv":["true"]},{"argv":["false"]}]},"lines":["true","false"]}
        {"event":"step","index":0,"status":"running"}
        {"event":"step","index":0,"status":"ok"}
        {"event":"step","index":1,"status":"failed","exit":1,"onFail":"left behind"}
        {"event":"done","ok":false}
        """
        let c = try fakeEngineroom(["api run start project-a": (events + "\n", 1)])
        var seen: [String] = []
        let ok = await c.run(["start", "project-a"]) { e in
            seen.append(e.event + (e.status.map { ":\($0)" } ?? ""))
        }
        XCTAssertFalse(ok)
        XCTAssertEqual(seen, ["plan", "step:running", "step:ok", "step:failed", "done"])
    }

    // engineroom is found by ENGINEROOM_PATH first, then in the usual install folders, never on the bare PATH
    // macOS starts apps with.
    func testLocate() {
        let have: Set<String> = ["/h/.local/bin/engineroom", "/opt/homebrew/bin/engineroom"]
        XCTAssertEqual(EngineRoomClient.locate(environment: ["HOME": "/h"], fileExists: have.contains), "/h/.local/bin/engineroom")
        XCTAssertEqual(EngineRoomClient.locate(environment: ["HOME": "/h", "ENGINEROOM_PATH": "/opt/homebrew/bin/engineroom"], fileExists: have.contains), "/opt/homebrew/bin/engineroom")
        XCTAssertNil(EngineRoomClient.locate(environment: ["HOME": "/nobody"], fileExists: { _ in false }))
    }

    // A run whose plan could not be made ends with the reason, so the window says why nothing
    // ran instead of only that it stopped. Fails if the reason is dropped.
    @MainActor
    func testARefusedRunCarriesItsReason() async throws {
        let c = try fakeEngineroom(["api run edit-project project-a": ("{\"error\":\"The editor command \\\"code\\\" was not found\",\"event\":\"done\",\"engineDown\":false,\"ok\":false}\n", 3)])
        var reason: String?
        let ok = await c.run(["edit-project", "project-a"]) { e in reason = e.error }
        XCTAssertFalse(ok)
        XCTAssertEqual(reason, "The editor command \"code\" was not found")
    }

    // engineroom runs with the environment the client was given, not the app's own, so the PATH found
    // for it reaches the commands engineroom runs. Fails if the environment is not passed on.
    func testEngineroomRunsInTheGivenEnvironment() async throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let exe = dir.appendingPathComponent("engineroom")
        try "#!/bin/sh\nprintf %s \"$PATH\"\n".write(to: exe, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: exe.path)
        let c = EngineRoomClient(executable: exe.path, environment: ["PATH": "/tools-a:/usr/bin:/bin"])
        let (out, _) = try await c.output(["x"])
        XCTAssertEqual(String(decoding: out, as: UTF8.self), "/tools-a:/usr/bin:/bin")
    }

    // The PATH given to engineroom is the login shell's, with the usual install folders added where it
    // lacks them; with no login shell answer, the app's own PATH and those folders. Fails if the
    // folder VS Code's command is linked into (/usr/local/bin) can be missing, or a folder repeats.
    func testThePathIsTheLoginShells() {
        XCTAssertEqual(EngineRoomClient.mergedPath(login: "/a:/usr/local/bin:/usr/bin", current: "/usr/bin:/bin", home: "/h", etcPaths: ["/usr/bin", "/bin"]),
                       "/a:/usr/local/bin:/usr/bin:/h/.local/bin:/opt/homebrew/bin:/bin")
        XCTAssertEqual(EngineRoomClient.mergedPath(login: nil, current: "/usr/bin:/bin:/usr/sbin:/sbin", home: "/h", etcPaths: ["/usr/local/bin", "/usr/bin"]),
                       "/usr/bin:/bin:/usr/sbin:/sbin:/h/.local/bin:/opt/homebrew/bin:/usr/local/bin")
        // With neither a login shell nor /etc/paths, the usual folders are still there.
        XCTAssertEqual(EngineRoomClient.mergedPath(login: "", current: "/usr/bin", home: "/h", etcPaths: []),
                       "/usr/bin:/h/.local/bin:/opt/homebrew/bin:/usr/local/bin")
    }

    // Zellij's variables, inherited when the app is opened from inside a Zellij pane, are not
    // passed on, so a window opened to join a session is not taken for one of its panes. Fails
    // if any ZELLIJ variable reaches engineroom, or the rest of the environment is lost.
    func testZellijsVariablesAreNotPassedOn() {
        let env = EngineRoomClient.toolEnvironment(base: ["HOME": "/h", "SHELL": "/nonexistent", "PATH": "/usr/bin",
                                                    "ZELLIJ": "0", "ZELLIJ_SESSION_NAME": "project-a", "ZELLIJ_PANE_ID": "0", "LANG": "en_NZ.UTF-8"])
        XCTAssertTrue(env.keys.filter { $0.hasPrefix("ZELLIJ") }.isEmpty, "\(env)")
        XCTAssertEqual(env["LANG"], "en_NZ.UTF-8")
        XCTAssertEqual(env["HOME"], "/h")
    }

    // A Claude Code session's markers, inherited when the app is opened by Claude, are not passed
    // on, while the developer's own Claude Code settings are. Fails if a marker reaches engineroom (a
    // session or editor it opens would take itself for that session's child) or a setting is lost.
    func testClaudeSessionMarkersAreNotPassedOn() {
        let env = EngineRoomClient.toolEnvironment(base: ["HOME": "/h", "SHELL": "/nonexistent", "PATH": "/usr/bin",
                                                    "CLAUDECODE": "1", "CLAUDE_CODE_CHILD_SESSION": "1", "CLAUDE_CODE_SESSION_ID": "x",
                                                    "CLAUDE_CODE_MESSAGING_SOCKET": "/tmp/s", "CLAUDE_CODE_MAX_OUTPUT_TOKENS": "kept"])
        for k in ["CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_MESSAGING_SOCKET"] {
            XCTAssertNil(env[k], k)
        }
        XCTAssertEqual(env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"], "kept")
    }

    // A project page's GitHub links decode: the repository's three pages, and a checkout's branch
    // with or without its pull requests (APP-PAGE-005, 006). Fails if a link is dropped on reading.
    func testGitHubLinksDecode() throws {
        let page = try JSONDecoder().decode(ProjectPage.self, from: Data("""
        {"name":"project-a","path":"/x","checkout":"~/x","settings":"~/s","slots":0,"scripts":[],
         "github":{"repo":"acme/widget","code":"https://github.com/acme/widget","issues":"https://github.com/acme/widget/issues","pulls":"https://github.com/acme/widget/pulls"},
         "checkouts":[{"session":"a","path":"/x","checkout":"~/x","branch":"main","main":true,"running":false,"ended":false,"changes":0,"github":{"code":"https://github.com/acme/widget/tree/main"}},
                      {"session":"b","path":"/y","checkout":"~/y","branch":"feature-x","main":false,"running":false,"ended":false,"changes":0,"github":{"code":"https://github.com/acme/widget/tree/feature-x","pulls":"https://github.com/acme/widget/pulls?q=is%3Apr+head%3Afeature-x"}},
                      {"session":"c","path":"/z","checkout":"~/z","branch":"local-only","main":false,"running":false,"ended":false,"changes":0}]}
        """.utf8))
        XCTAssertEqual(page.github?.repo, "acme/widget")
        XCTAssertEqual(page.github?.issues, "https://github.com/acme/widget/issues")
        XCTAssertEqual(page.github?.pulls, "https://github.com/acme/widget/pulls")
        XCTAssertNil(page.checkouts[0].github?.pulls)
        XCTAssertEqual(page.checkouts[1].github?.pulls, "https://github.com/acme/widget/pulls?q=is%3Apr+head%3Afeature-x")
        XCTAssertNil(page.checkouts[2].github)
    }

    /// Sessions as the engine lists them, the waiting ones given.
    private func sessions(_ rows: [(name: String, project: String, waiting: Bool, assistant: Bool, here: Bool)]) throws -> [Session] {
        let json = rows.map { r in
            """
            {"name":"\(r.name)","project":"\(r.project)","ended":false,"waiting":"\(r.waiting ? "waiting" : "")"\
            \(r.waiting ? ",\"question\":\"Approve the plan?\"" : "")\(r.assistant ? ",\"assistant\":true" : "")\(r.here ? ",\"here\":true" : "")}
            """
        }.joined(separator: ",")
        return try JSONDecoder().decode([Session].self, from: Data("[\(json)]".utf8))
    }

    // Built step by step: one long chain of + over mapped arrays is too slow for some compilers to
    // type-check.
    private func names(_ list: PanelList) -> [String] {
        var out: [String] = []
        for p in list.pinned {
            out += p.sessions.isEmpty ? ["(start \(p.project))"] : p.sessions.map(\.name)
        }
        out.append("|")
        out += list.running.map(\.name)
        out.append("|")
        out += list.helpers.map(\.name)
        out.append("|")
        out += list.here.map(\.name)
        return out
    }

    // The panel lists the sessions in one order whether or not Claude is waiting in any of them, the
    // waiting one marked on its own row (APP-LIST-001). Fails if a waiting session is moved (to the
    // top, or into a section of its own), or listed twice.
    func testWaitingDoesNotMoveASession() throws {
        let rows: [(name: String, project: String, waiting: Bool, assistant: Bool, here: Bool)] = [
            ("project-b", "project-b", false, false, false), ("project-a-fix", "project-a", false, false, false),
            ("project-a", "project-a", false, false, false), ("project-c", "project-c", false, false, false),
            ("project-a-ai", "project-a", false, true, false),
        ]
        let quiet = PanelList(sessions: try sessions(rows), pinned: ["project-b"])
        for i in rows.indices {
            var waiting = rows
            waiting[i].waiting = true
            let list = PanelList(sessions: try sessions(waiting.reversed()), pinned: ["project-b"])
            XCTAssertEqual(names(list), names(quiet), "with \(rows[i].name) waiting")
            let marked = (list.pinned.flatMap(\.sessions) + list.running + list.helpers).filter(\.isWaiting)
            XCTAssertEqual(marked.map(\.name), [rows[i].name])
            XCTAssertEqual(marked.first?.question, "Approve the plan?")
        }
    }

    // Pinned projects come first, by name, each always listed: with its running sessions, main clone
    // first, or as one to start (APP-LIST-002). Then the rest by name, then Ask Claude, then this
    // Mac's own. Fails if a pinned project drops out or moves, or the order follows the engine's.
    func testPinnedProjectsAreAlwaysFirstInAFixedOrder() throws {
        let list = PanelList(sessions: try sessions([
            ("zeta", "zeta", false, false, false), ("Beta-work", "Beta", false, false, false),
            ("alpha", "alpha", true, false, false), ("Beta", "Beta", false, false, false),
            ("alpha-ai", "alpha", false, true, false), ("mine", "mine", false, false, true),
        ]), pinned: ["Beta", "idle", "Beta"])
        XCTAssertEqual(names(list), ["Beta", "Beta-work", "(start idle)", "|", "alpha", "zeta", "|", "alpha-ai", "|", "mine"])
    }

    // A session carries its branch's links on GitHub and the repositories list carries each
    // project's repository, for the panel's GitHub line (APP-LIST-003). Fails if either is dropped
    // on reading.
    func testThePanelsGitHubLinksDecode() throws {
        let s = try JSONDecoder().decode(Session.self, from: Data("""
        {"name":"project-a-fix","project":"project-a","ended":false,"waiting":"",
         "github":{"code":"https://github.com/acme/widget/tree/fix","pulls":"https://github.com/acme/widget/pulls?q=is%3Apr+head%3Afix"}}
        """.utf8))
        XCTAssertEqual(s.github?.code, "https://github.com/acme/widget/tree/fix")
        XCTAssertNotNil(s.github?.pulls)
        let r = try JSONDecoder().decode(ReposReply.self, from: Data("""
        {"roots":[],"projects":[],"github":{"project-a":{"repo":"acme/widget","code":"https://github.com/acme/widget","issues":"https://github.com/acme/widget/issues","pulls":"https://github.com/acme/widget/pulls"}}}
        """.utf8))
        XCTAssertEqual(r.github?["project-a"]?.issues, "https://github.com/acme/widget/issues")
        XCTAssertNil(try JSONDecoder().decode(ReposReply.self, from: Data(#"{"roots":[],"projects":[]}"#.utf8)).github)
    }

    // The login shell's PATH is read from between its markers, whatever the profile prints, and a
    // shell that does not answer in time gives nil instead of holding the app. Fails if profile
    // noise is taken as the PATH or a hung shell is waited on.
    // The menu bar carries Claude's mark while any running session waits, and not otherwise
    // (APP-FEED-005). Fails if the mark shows with none waiting, misses a waiting one, or shows for
    // a session that has ended.
    func testTheMenuBarMarkFollowsWaiting() throws {
        let quiet = try sessions([("project-a", "project-a", false, false, false), ("project-b", "project-b", false, false, false)])
        XCTAssertFalse(PanelList.anyWaiting(quiet))
        XCTAssertFalse(PanelList.anyWaiting([]))
        let one = try sessions([("project-a", "project-a", false, false, false), ("project-b", "project-b", true, false, false)])
        XCTAssertTrue(PanelList.anyWaiting(one))
        let ended = try EngineRoomClient.decode([Session].self, data: Data("""
        [{"name":"project-a","ended":true,"waiting":"waiting","question":"Approve the plan?"}]
        """.utf8), code: 0)
        XCTAssertFalse(PanelList.anyWaiting(ended))
    }

    // Each line of the feed is read for what it is: the sessions, the engine not answering, or a
    // keep-alive; anything else is ignored rather than shown (D-023). Fails if a list or an error
    // is misread, or a keep-alive is taken for an empty list.
    func testFeedLinesAreRead() throws {
        guard case .sessions(let r)? = Feed.parse(Data(#"{"sessions":[{"name":"project-a","ended":false,"waiting":"waiting","question":"Approve the plan?"}],"read":"zellij list-sessions","where":"engine"}"#.utf8)) else {
            return XCTFail("a list was not read as one")
        }
        XCTAssertEqual(r.sessions.map(\.name), ["project-a"])
        XCTAssertEqual(r.sessions.first?.question, "Approve the plan?")
        guard case .problem(let e)? = Feed.parse(Data(#"{"error":"the engine did not answer","engineDown":true}"#.utf8)) else {
            return XCTFail("an error was not read as one")
        }
        XCTAssertEqual(e.engineDown, true)
        guard case .quiet? = Feed.parse(Data()) else { return XCTFail("a keep-alive was not read as one") }
        XCTAssertNil(Feed.parse(Data("not json".utf8)))
    }

    // A refusal that something would make safe carries its offers to the app (D-024), and a role
    // worktree its role. Fails if either is dropped in decoding.
    func testRefusalOffersAndRolesDecode() throws {
        let data = Data(#"{"error":"project-a-x has uncommitted changes","engineDown":false,"offers":[{"label":"Put them aside","action":["put-aside","project-a","/src/project-a-x"]}]}"#.utf8)
        do {
            _ = try EngineRoomClient.decode(PlanReply.self, data: data, code: 3)
            XCTFail("a refusal was read as a plan")
        } catch let e as APIError {
            XCTAssertEqual(e.offers?.first?.action, ["put-aside", "project-a", "/src/project-a-x"])
        }
        let c = try JSONDecoder().decode(CheckoutView.self, from: Data(#"{"session":"project-a-designer","path":"/p","checkout":"~/p","branch":"nav-rework","main":false,"work":"designer","role":"designer","running":false,"ended":false,"changes":0}"#.utf8))
        XCTAssertEqual(c.role, "designer")
    }

    // The panel's list is one row per project, pinned first, each with one light for all its
    // sessions, and a chosen project shows every checkout, running or not (APP-LIST-004, D-025).
    // Fails if a pinned project with nothing running drops out, the order follows waiting, the
    // light ignores a waiting session, or a checkout the feed sent is missing from the cards.
    func testThePanelListsOneRowPerProjectWithOneLight() throws {
        let list = PanelList(sessions: try sessions([
            ("gamma", "gamma", false, false, false), ("alpha-work", "alpha", true, false, false),
            ("alpha", "alpha", false, false, false),
        ]), pinned: ["beta"])
        let facts = try JSONDecoder().decode([String: [CheckoutFacts]].self, from: Data(#"""
        {"alpha":[{"session":"alpha-work","path":"/a-work","main":false,"work":"work","branch":"work","changes":1,"unpushed":0,"behind":0},
                  {"session":"alpha","path":"/a","main":true,"branch":"main","changes":0,"unpushed":0,"behind":0},
                  {"session":"alpha-old","path":"/a-old","main":false,"work":"old","branch":"old","changes":0,"unpushed":0,"behind":0,"merged":true}]}
        """#.utf8))
        let projects = PanelProject.list(list, checkouts: facts)
        XCTAssertEqual(projects.map(\.project), ["beta", "alpha", "gamma"])
        XCTAssertEqual(projects.map(\.light), [.idle, .waiting, .running])
        let alpha = projects[1]
        XCTAssertEqual(alpha.cards.map(\.id), ["alpha", "alpha-old", "alpha-work"])
        XCTAssertEqual(alpha.cards.map(\.light), [.running, .idle, .waiting])
        XCTAssertTrue(projects[0].cards.isEmpty)
        // The project last chosen stays listed after its last session stops, last and marked as
        // recent, so the panel comes back to it; one already listed is not listed twice.
        let kept = PanelProject.list(list, checkouts: facts, keep: "delta")
        XCTAssertEqual(kept.map(\.project), ["beta", "alpha", "gamma", "delta"])
        XCTAssertEqual(kept.map(\.recent), [false, false, false, true])
        XCTAssertEqual(PanelProject.list(list, checkouts: facts, keep: "alpha").map(\.project), ["beta", "alpha", "gamma"])
    }

    /// The checkouts the feed sends for the keyboard tests: alpha with a main clone and two
    /// worktrees, one of them a role's; beta with a main clone and a worktree.
    private func keyFacts() throws -> [String: [CheckoutFacts]] {
        try JSONDecoder().decode([String: [CheckoutFacts]].self, from: Data(#"""
        {"alpha":[{"session":"alpha","path":"/a","main":true,"branch":"main","changes":0,"unpushed":0,"behind":0},
                  {"session":"alpha-old","path":"/a-old","main":false,"work":"old","branch":"old-fix","changes":0,"unpushed":0,"behind":0},
                  {"session":"alpha-work","path":"/a-work","main":false,"work":"work","branch":"work","changes":0,"unpushed":0,"behind":0}],
         "beta":[{"session":"beta","path":"/b","main":true,"branch":"main","changes":0,"unpushed":0,"behind":0},
                 {"session":"beta-designer","path":"/b-designer","main":false,"role":"designer","branch":"nav","changes":0,"unpushed":0,"behind":0}]}
        """#.utf8))
    }

    // The arrow keys walk the panel's rows in the order they are drawn and stop at each end
    // (D-033). Fails if a key skips a row, wraps round from the last row to the first, moves off
    // the first row when nothing was chosen (the first row is the one shown then), or answers with
    // a row for an empty list.
    func testArrowKeysWalkThePanelsRowsAndStopAtTheEnds() throws {
        let list = PanelList(sessions: try sessions([
            ("gamma", "gamma", false, false, false), ("alpha", "alpha", false, false, false),
        ]), pinned: ["beta"])
        let rows = PanelProject.list(list, checkouts: [:], keep: "delta")
        XCTAssertEqual(rows.map(\.id), ["beta", "alpha", "gamma", "delta"])
        XCTAssertEqual(PanelProject.step(rows, from: "beta", by: 1), "alpha")
        XCTAssertEqual(PanelProject.step(rows, from: "alpha", by: 1), "gamma")
        XCTAssertEqual(PanelProject.step(rows, from: "gamma", by: 1), "delta")
        XCTAssertEqual(PanelProject.step(rows, from: "delta", by: 1), "delta")
        XCTAssertEqual(PanelProject.step(rows, from: "delta", by: -1), "gamma")
        XCTAssertEqual(PanelProject.step(rows, from: "beta", by: -1), "beta")
        // Nothing chosen, or a choice that is no longer listed: the first row is the one shown,
        // so down moves to the second and up stays on the first.
        XCTAssertEqual(PanelProject.shown(rows, chosen: nil)?.id, "beta")
        XCTAssertEqual(PanelProject.shown(rows, chosen: "gone")?.id, "beta")
        XCTAssertEqual(PanelProject.shown(rows, chosen: "gamma")?.id, "gamma")
        XCTAssertEqual(PanelProject.step(rows, from: nil, by: 1), "alpha")
        XCTAssertEqual(PanelProject.step(rows, from: "gone", by: 1), "alpha")
        XCTAssertEqual(PanelProject.step(rows, from: nil, by: -1), "beta")
        // One row: both keys stay on it. No rows: there is nowhere to go.
        XCTAssertEqual(PanelProject.step([rows[0]], from: "beta", by: 1), "beta")
        XCTAssertEqual(PanelProject.step([rows[0]], from: "beta", by: -1), "beta")
        XCTAssertNil(PanelProject.step([], from: "beta", by: 1))
        XCTAssertNil(PanelProject.shown([], chosen: "beta"))
    }

    // Return joins the session Claude waits in, before any other that runs, and a running one
    // before starting anything (D-033). Fails if Return joins the first running session while
    // another of the project waits, starts a checkout while one of the project's sessions runs,
    // or the card marked for Return is not the one acted on.
    func testReturnJoinsTheWaitingSessionThenARunningOne() throws {
        let facts = try keyFacts()
        // alpha's main clone and its "work" worktree run; the worktree waits.
        let waiting = PanelProject.list(PanelList(sessions: try sessions([
            ("alpha", "alpha", false, false, false), ("alpha-work", "alpha", true, false, false),
        ]), pinned: []), checkouts: facts)[0]
        XCTAssertEqual(waiting.cards.map(\.id), ["alpha", "alpha-old", "alpha-work"])
        XCTAssertEqual(waiting.keyCard?.id, "alpha-work")
        guard case .join(let joined) = waiting.keyAction else { return XCTFail("Return did not join") }
        XCTAssertEqual(joined.name, "alpha-work")
        // Nothing waits: the first card that runs, in the order drawn, past one that does not.
        let running = PanelProject.list(PanelList(sessions: try sessions([
            ("alpha-old", "alpha", false, false, false), ("alpha-work", "alpha", false, false, false),
        ]), pinned: []), checkouts: facts)[0]
        XCTAssertEqual(running.keyCard?.id, "alpha-old")
        guard case .join(let first) = running.keyAction else { return XCTFail("Return did not join") }
        XCTAssertEqual(first.name, "alpha-old")
        // A session on this Mac is joined on this Mac: its target carries --here.
        let here = try sessions([("project-a", "project-a", false, false, true)])
        XCTAssertEqual(here[0].target, ["project-a", "--here"])
    }

    // With nothing of the project running, Return starts what the first card's Start starts: the
    // main clone by project, a worktree by its work name, a role's worktree by its role; a
    // project with no cards is started by its name (D-033). Fails if a worktree is started as the
    // main clone (which would start a second session of the project in the wrong folder), or a
    // pinned project whose checkouts have not arrived cannot be started from the keyboard.
    func testReturnStartsWhatTheCardsStartButtonStarts() throws {
        let facts = try keyFacts()
        let idle = PanelProject.list(PanelList(sessions: [], pinned: ["alpha", "beta", "gamma"]), checkouts: facts)
        XCTAssertEqual(idle.map(\.id), ["alpha", "beta", "gamma"])
        XCTAssertEqual(idle[0].keyCard?.id, "alpha")
        XCTAssertEqual(idle[0].keyAction, .start(["start", "alpha"]))
        // Each card's own Start, which the button sends too.
        XCTAssertEqual(idle[0].cards.map { $0.startAction(project: "alpha") },
                       [["start", "alpha"], ["new", "alpha", "old"], ["new", "alpha", "work"]])
        XCTAssertEqual(idle[1].cards.map { $0.startAction(project: "beta") },
                       [["start", "beta"], ["new", "beta", "designer"]])
        XCTAssertEqual(idle[0].cards.map { $0.isMain(of: "alpha") }, [true, false, false])
        // No checkouts sent for it yet: no card, and Return starts the project itself.
        XCTAssertNil(idle[2].keyCard)
        XCTAssertEqual(idle[2].keyAction, .start(["start", "gamma"]))
        // A running session the feed sent no checkout for is the main clone only when it carries
        // the project's own name.
        let bare = PanelProject.list(PanelList(sessions: try sessions([
            ("delta", "delta", false, false, false), ("delta-x", "delta", false, false, false),
        ]), pinned: []), checkouts: [:])[0]
        XCTAssertEqual(bare.cards.map { $0.isMain(of: "delta") }, [true, false])
    }

    // Which key is the panel's. Fails if a key code is mapped to the wrong key (Down walking up
    // the list), or if any other key, a letter typed or Tab, is taken from the window.
    func testOnlyTheArrowsReturnAndEscapeAreThePanelsKeys() {
        XCTAssertEqual(PanelKey(keyCode: 125), .down)
        XCTAssertEqual(PanelKey(keyCode: 126), .up)
        XCTAssertEqual(PanelKey(keyCode: 36), .enter)
        XCTAssertEqual(PanelKey(keyCode: 76), .enter, "the keypad's Enter")
        XCTAssertEqual(PanelKey(keyCode: 53), .escape)
        for other: UInt16 in [0, 48, 49, 51, 123, 124] {
            XCTAssertNil(PanelKey(keyCode: other), "key code \(other) is not the panel's")
        }
    }

    // The keys of the panel's list. Fails if an arrow moves the wrong way, Return stops joining,
    // Escape stops closing the panel, or the engine-down screen (which has no list) starts
    // answering the arrows and Return with a list that is not on screen.
    func testThePanelsListAnswersTheArrowsReturnAndEscape() {
        let list = PanelKey.Place(inPanel: true, screen: .list)
        XCTAssertEqual(PanelKey.down.effect(at: list), .step(1))
        XCTAssertEqual(PanelKey.up.effect(at: list), .step(-1))
        XCTAssertEqual(PanelKey.enter.effect(at: list), .act)
        XCTAssertEqual(PanelKey.escape.effect(at: list), .closePanel)

        let down = PanelKey.Place(inPanel: true, screen: .engineDown)
        XCTAssertEqual(PanelKey.down.effect(at: down), .pass)
        XCTAssertEqual(PanelKey.up.effect(at: down), .pass)
        XCTAssertEqual(PanelKey.enter.effect(at: down), .pass)
        XCTAssertEqual(PanelKey.escape.effect(at: down), .closePanel)
    }

    // Escape is the link back on a page reached from another, and nothing else is taken there.
    // Fails if Escape on an action's page closes the panel instead of the page, if Return or an
    // arrow acts on the list hidden behind an action's page, or if Escape is taken on a page of
    // the window that has nowhere to go back to.
    func testEscapeIsTheLinkBackOnAPageReachedFromAnother() {
        let panelAction = PanelKey.Place(inPanel: true, screen: .action)
        XCTAssertEqual(PanelKey.escape.effect(at: panelAction), .back)
        for key in [PanelKey.up, .down, .enter] {
            XCTAssertEqual(key.effect(at: panelAction), .pass, "\(key) behind an action's page")
        }
        XCTAssertEqual(PanelKey.escape.effect(at: .init(inPanel: false, screen: .action)), .back)
        XCTAssertEqual(PanelKey.escape.effect(at: .init(inPanel: false, screen: .task)), .back)
        XCTAssertEqual(PanelKey.escape.effect(at: .init(inPanel: false, screen: .other)), .pass)
        // The window's lists and forms keep their own arrows and Return on every page.
        for screen in [PanelKey.Screen.action, .task, .other] {
            for key in [PanelKey.up, .down, .enter] {
                XCTAssertEqual(key.effect(at: .init(inPanel: false, screen: screen)), .pass, "\(key) in the window, \(screen)")
            }
        }
        // From one of the page's fields too, as Escape cancels a dialog while a field is typed into.
        XCTAssertEqual(PanelKey.escape.effect(at: .init(inPanel: false, screen: .task, typing: true)), .back)
    }

    // What the keys must leave alone. Fails if a shortcut's key is taken (Command-Down), if a
    // dialog's Return or Escape is taken from under it, if the next-branch field in the panel
    // loses its Return, arrows or Escape to the list, or if Escape closes a page while an input
    // method is part way through a character.
    func testKeysThatBelongToSomethingElseAreLeftAlone() {
        let all = [PanelKey.up, .down, .enter, .escape]
        let places: [PanelKey.Place] = [
            .init(inPanel: true, screen: .list), .init(inPanel: true, screen: .action),
            .init(inPanel: false, screen: .action), .init(inPanel: false, screen: .task),
        ]
        for place in places {
            for key in all {
                var modified = place; modified.modified = true
                XCTAssertEqual(key.effect(at: modified), .pass, "\(key) with a modifier, \(place.screen)")
                var dialog = place; dialog.dialog = true
                XCTAssertEqual(key.effect(at: dialog), .pass, "\(key) under a dialog, \(place.screen)")
            }
            var composing = place; composing.composing = true; composing.typing = true
            XCTAssertEqual(PanelKey.escape.effect(at: composing), .pass, "Escape while composing, \(place.screen)")
        }
        let field = PanelKey.Place(inPanel: true, screen: .list, typing: true)
        for key in all {
            XCTAssertEqual(key.effect(at: field), .pass, "\(key) while the panel's field is typed into")
        }
    }

    // A held key. Fails if holding Return asks to join again and again, if holding Escape goes
    // back from an action's page and then on out of the panel, or if a held arrow stops walking.
    func testAHeldKeyWalksTheListButActsAndGoesBackOnce() {
        let list = PanelKey.Place(inPanel: true, screen: .list, repeated: true)
        XCTAssertEqual(PanelKey.down.effect(at: list), .step(1))
        XCTAssertEqual(PanelKey.up.effect(at: list), .step(-1))
        XCTAssertEqual(PanelKey.enter.effect(at: list), .taken)
        XCTAssertEqual(PanelKey.escape.effect(at: list), .taken)
        XCTAssertEqual(PanelKey.escape.effect(at: .init(inPanel: true, screen: .action, repeated: true)), .taken)
        XCTAssertEqual(PanelKey.escape.effect(at: .init(inPanel: true, screen: .engineDown, repeated: true)), .taken)
    }

    // A project's light outside the panel is one light for all its sessions: Claude's mark when
    // one waits, running when one runs, off with none; an Ask Claude session and one on this Mac
    // count, another project's do not, and one that ended neither runs nor waits (D-015, D-032).
    // Fails if a waiting session is shown as merely running, another project's session lights
    // this one, or an ended session keeps a project lit or marked.
    func testAProjectsLightIsOneLightForAllItsSessions() throws {
        let feed = try sessions([
            ("project-a", "project-a", false, false, false), ("project-a-work", "project-a", true, false, false),
            ("project-b", "project-b", false, false, false),
            ("project-c-ask", "project-c", true, true, false), ("project-d", "project-d", false, false, true),
        ])
        XCTAssertEqual(PanelLight.of(project: "project-a", in: feed), .waiting)
        XCTAssertEqual(PanelLight.of(project: "project-b", in: feed), .running)
        XCTAssertEqual(PanelLight.of(project: "project-c", in: feed), .waiting)
        XCTAssertEqual(PanelLight.of(project: "project-d", in: feed), .running)
        XCTAssertEqual(PanelLight.of(project: "project-e", in: feed), .idle)
        XCTAssertEqual(PanelLight.of(project: "project-a", in: []), .idle)
        let ended = try EngineRoomClient.decode([Session].self, data: Data("""
        [{"name":"project-a","project":"project-a","ended":true,"waiting":"waiting","question":"Approve the plan?"}]
        """.utf8), code: 0)
        XCTAssertEqual(PanelLight.of(project: "project-a", in: ended), .idle)
    }

    func testTheLoginShellIsRead() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let noisy = dir.appendingPathComponent("noisy")
        try "#!/bin/sh\necho welcome back\nPATH=/from-profile:/usr/bin\nexport PATH\nexec /bin/sh -c \"$2\"\n".write(to: noisy, atomically: true, encoding: .utf8)
        let hung = dir.appendingPathComponent("hung")
        try "#!/bin/sh\nexec sleep 30\n".write(to: hung, atomically: true, encoding: .utf8)
        for f in [noisy, hung] { try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: f.path) }
        XCTAssertEqual(EngineRoomClient.loginShellPath(shell: noisy.path), "/from-profile:/usr/bin")
        let start = Date()
        XCTAssertNil(EngineRoomClient.loginShellPath(shell: hung.path, timeout: 0.5))
        XCTAssertLessThan(Date().timeIntervalSince(start), 5)
        XCTAssertNil(EngineRoomClient.extractPath("no markers here"))
    }

    // Copy all joins so a paste stops at the first failure (LEARN-COPY-002).
    func testJoinAll() {
        XCTAssertEqual(joinAll(["a", "b"]), "a &&\nb\n")
    }

    // The line splitter keeps a partial line for the next chunk.
    func testLineSplitter() {
        let s = LineSplitter()
        XCTAssertEqual(s.feed(Data("ab\ncd".utf8)).map { String(decoding: $0, as: UTF8.self) }, ["ab"])
        XCTAssertEqual(s.feed(Data("e\n".utf8)).map { String(decoding: $0, as: UTF8.self) }, ["cde"])
    }

    // A client newer than its engine was told to update itself: the banner always named this Mac.
    // Fails if the machine told to update is not the one behind, if builds from different branches
    // tell one side to update, or if an answer that cannot order them (or an older engineroom that
    // leaves `behind` out) tells either side to update.
    func testTheBannerTellsTheMachineThatIsBehind() throws {
        let engineBehind = try EngineRoomClient.decode(EngineVersion.self, data: Data(#"{"here":"bbb2222","engine":"aaa1111","behind":"engine"}"#.utf8), code: 0)
        XCTAssertEqual(engineBehind.advice, "This Mac runs engineroom bbb2222, newer than the engine's aaa1111. Update the engine: git pull, then scripts/install.sh --app, on the engine.")
        XCTAssertEqual(EngineVersion.advice(here: "aaa1111", engine: "bbb2222", behind: "here"), "The engine runs engineroom bbb2222, newer than this Mac's aaa1111. Update this Mac: git pull, then scripts/install.sh --app.")
        XCTAssertFalse(EngineVersion.advice(here: "aaa1111", engine: "ccc3333", behind: "apart")!.contains("Update this Mac"))
        XCTAssertFalse(EngineVersion.advice(here: "aaa1111", engine: "ccc3333", behind: "apart")!.contains("Update the engine"))
        // CONN-ROLE-003: the engine's own machine name is read when it gives one, and is nil from
        // an engine too old to. Fails if the name is dropped or an answer without it cannot be read.
        let named = try EngineRoomClient.decode(EngineVersion.self, data: Data(#"{"here":"aaa1111","engine":"aaa1111","behind":"none","name":"Studio Mac"}"#.utf8), code: 0)
        XCTAssertEqual(named.name, "Studio Mac")
        let older = try EngineRoomClient.decode(EngineVersion.self, data: Data(#"{"here":"aaa1111","engine":"bbb2222"}"#.utf8), code: 0)
        XCTAssertNil(older.name)
        XCTAssertEqual(older.advice, "The engine runs engineroom bbb2222 and this Mac runs aaa1111.")
        XCTAssertEqual(EngineVersion.advice(here: "aaa1111", engine: "bbb2222", behind: "unknown"), "The engine runs engineroom bbb2222 and this Mac runs aaa1111.")
        XCTAssertNil(EngineVersion.advice(here: "aaa1111", engine: "aaa1111", behind: "none"))
        XCTAssertNil(EngineVersion.advice(here: "aaa1111", engine: "aaa1111", behind: nil))
    }

    // MARK: health (HEALTH-PAGE-001, HEALTH-FIX-001, HEALTH-FIX-002)

    /// A health report as `engineroom health --json` writes it with items missing: setup, staying
    /// awake, and two sessions, one with an app the program can start.
    private let healthMissing = """
    {"machine":"engine.example","missing":2,"items":[
     {"name":"zellij","for":"both","present":true,"checked":"zellij --version","found":"zellij 0.43.1","group":"setup"},
     {"name":"stays awake on power","for":"engine","present":false,"checked":"pmset -g custom (sleep under AC Power)","found":"sleep 1 on power","fix":"sudo pmset -c sleep 0","group":"awake"},
     {"name":"Remote Control in project-a","for":"engine","present":true,"checked":"sessions/<pid>.json","found":"on","group":"session","session":"project-a"},
     {"name":"the app of project-a","for":"engine","present":false,"checked":".engineroom/status","found":"not running: api down","fix":"start the app: engineroom up project-a","group":"session","session":"project-a","action":["up","project-a"]},
     {"name":"the app of project-b","for":"engine","present":true,"checked":"the project's scripts","found":"no app to run","group":"session","session":"project-b"}
    ]}
    """

    // The check ends with a failure when anything is missing, and its report is still the answer:
    // the page shows the items, not an error. Fails if a failing exit is read as a refusal, or if
    // the items, their fixes or the action do not come through.
    func testHealthWithMissingItemsIsReadAsTheReport() async throws {
        let c = try fakeEngineroom(["health --json": (healthMissing, 1)])
        let r = try await c.health()
        XCTAssertEqual(r.machine, "engine.example")
        XCTAssertEqual(r.missing, 2)
        XCTAssertFalse(r.ready)
        XCTAssertEqual(r.items.count, 5)
        XCTAssertEqual(r.items[1].fix, "sudo pmset -c sleep 0")
        XCTAssertNil(r.items[1].action)
        XCTAssertEqual(r.items[3].action, ["up", "project-a"])
    }

    // Every item present: ready, as the command's success says.
    func testHealthAllPresentIsReady() throws {
        let r = try EngineRoomClient.decode(HealthReport.self, data: Data("""
        {"machine":"this machine","missing":0,"items":[{"name":"git","for":"both","present":true,"checked":"git --version","found":"git 2.50","group":"setup"}]}
        """.utf8), code: 0)
        XCTAssertTrue(r.ready)
        XCTAssertNil(r.items[0].fix)
    }

    // An engine not reached from a client: engineroom writes its items as null. The report still
    // reads, with no item and not ready, and the cause is kept (HEALTH-CHECK-003). Fails if null
    // items make the answer unreadable, which would show a decoding error in place of the cause.
    func testHealthUnreachedReadsWithItsCause() async throws {
        let c = try fakeEngineroom(["health --json": ("""
        {"machine":"engine.example","unreached":"The engine did not answer. ssh: connect to host engine.example port 22: Operation timed out","items":null,"missing":0}
        """, 1)])
        let r = try await c.health()
        XCTAssertTrue(r.items.isEmpty)
        XCTAssertFalse(r.ready)
        XCTAssertEqual(r.unreached?.hasSuffix("Operation timed out"), true)
    }

    // The page lays the items out under the command's headings, in its order, each session's under
    // its own name. Fails if items are regrouped, reordered, or sessions run together.
    func testHealthSectionsFollowTheReport() throws {
        let r = try EngineRoomClient.decode(HealthReport.self, data: Data(healthMissing.utf8), code: 1)
        let s = r.sections
        XCTAssertEqual(s.map(\.title), ["Setup", "Staying awake", "project-a", "project-b"])
        XCTAssertEqual(s.map(\.isSession), [false, false, true, true])
        XCTAssertEqual(s[2].items.map(\.name), ["Remote Control in project-a", "the app of project-a"])
    }

    // Only a missing item whose fix the program can run is offered as an action (HEALTH-FIX-001);
    // a fix to type is never runnable (HEALTH-FIX-002); and a client working local, whose actions
    // run on this Mac, is offered none for the engine's report. Fails if any of the three is
    // offered a button.
    func testHealthOffersOnlyTheFixesTheProgramRunsWhereItWasChecked() throws {
        let r = try EngineRoomClient.decode(HealthReport.self, data: Data(healthMissing.utf8), code: 1)
        XCTAssertEqual(r.items[3].runnableFix(workingLocal: false), ["up", "project-a"])
        XCTAssertNil(r.items[3].runnableFix(workingLocal: true))
        XCTAssertNil(r.items[1].runnableFix(workingLocal: false))
        XCTAssertNil(r.items[0].runnableFix(workingLocal: false))
    }
}
