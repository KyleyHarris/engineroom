import Foundation

/// Runs `engineroom` and reads its JSON. The app does nothing else: every list and action goes through
/// here, so the app can never act differently from the program (docs/decisions.md D-008).
public final class EngineRoomClient: @unchecked Sendable {
    public let executable: String
    /// The environment engineroom runs in. Its PATH is the one a terminal has, so the commands engineroom
    /// runs for the app (code, zellij, git, ssh) are found as they are when typed: macOS starts
    /// the app with a bare PATH, which holds none of the folders those are installed in.
    public let environment: [String: String]

    public init(executable: String, environment: [String: String]? = nil) {
        self.executable = executable
        self.environment = environment ?? Self.toolEnvironment()
    }

    /// The app's environment with PATH as the developer's login shell has it, and without
    /// Zellij's variables. The app is never inside a Zellij pane, but it inherits them when it is
    /// opened from a terminal that is, and a window it opens to join a session would then tell
    /// Zellij it is already inside that session, which Zellij refuses to join. Nor does it pass on
    /// a Claude Code session's markers, inherited when the app is opened by Claude: whatever the
    /// app opens (a session, VS Code, a terminal) would take itself for a child of that session,
    /// and a Claude started there would save no transcript. The names are engineroom's own list
    /// (internal/zj AgentSessionVars).
    public static let agentSessionVars: Set<String> = [
        "CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_SESSION_ATTENDED",
        "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_MESSAGING_SOCKET",
        "CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_CODE_BRIDGE_SESSION_ID",
    ]

    public static func toolEnvironment(base: [String: String] = ProcessInfo.processInfo.environment) -> [String: String] {
        var env = base.filter { !$0.key.hasPrefix("ZELLIJ") && !agentSessionVars.contains($0.key) }
        let home = base["HOME"] ?? NSHomeDirectory()
        let etc = (try? String(contentsOfFile: "/etc/paths", encoding: .utf8)) ?? ""
        env["PATH"] = mergedPath(login: loginShellPath(shell: base["SHELL"] ?? "/bin/zsh"),
                                 current: base["PATH"], home: home,
                                 etcPaths: etc.split(separator: "\n").map(String.init))
        return env
    }

    /// The PATH engineroom is given: the login shell's when it could be read, else the app's own; then
    /// any of the usual install folders it lacks, so a shell that could not be read still finds
    /// what is installed in the usual places. Folders keep their first place and appear once.
    public static func mergedPath(login: String?, current: String?, home: String, etcPaths: [String]) -> String {
        var out: [String] = []
        func add(_ dir: String) {
            let d = dir.trimmingCharacters(in: .whitespaces)
            if !d.isEmpty && !out.contains(d) { out.append(d) }
        }
        let first = (login?.isEmpty == false ? login : current) ?? ""
        first.split(separator: ":").forEach { add(String($0)) }
        ["\(home)/.local/bin", "/opt/homebrew/bin", "/usr/local/bin"].forEach(add)
        etcPaths.forEach(add)
        return out.joined(separator: ":")
    }

    /// The PATH the developer's login shell sets, read by running it once as a terminal would
    /// (login and interactive, so both its profile and its rc file are read), or nil when it does
    /// not answer within a few seconds or answers with nothing. The PATH is printed between
    /// markers, so anything the profile itself prints is ignored.
    static func loginShellPath(shell: String, timeout: TimeInterval = 3) -> String? {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: shell)
        p.arguments = ["-ilc", "printf '<<engineroom-path>>%s<<end>>' \"$PATH\""]
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        p.standardInput = FileHandle.nullDevice
        let ended = DispatchSemaphore(value: 0)
        p.terminationHandler = { _ in ended.signal() }
        do { try p.run() } catch { return nil }
        // Read while it runs, so a profile that prints a lot cannot fill the pipe and stall.
        var data = Data()
        let reader = DispatchQueue(label: "engineroom.login-path")
        let read = DispatchSemaphore(value: 0)
        reader.async { data = out.fileHandleForReading.readDataToEndOfFile(); read.signal() }
        if ended.wait(timeout: .now() + timeout) == .timedOut {
            p.terminate()
            _ = read.wait(timeout: .now() + 1)
            return nil
        }
        _ = read.wait(timeout: .now() + 1)
        return extractPath(String(decoding: data, as: UTF8.self))
    }

    /// The PATH between the markers in a shell's output, or nil.
    static func extractPath(_ text: String) -> String? {
        guard let a = text.range(of: "<<engineroom-path>>"),
              let b = text.range(of: "<<end>>", range: a.upperBound..<text.endIndex) else { return nil }
        let path = String(text[a.upperBound..<b.lowerBound])
        return path.isEmpty ? nil : path
    }

    /// Where engineroom is: ENGINEROOM_PATH, or the first of the usual install folders that holds it. The
    /// app is started by macOS with a bare PATH, so the shell's PATH cannot be relied on.
    public static func locate(environment: [String: String] = ProcessInfo.processInfo.environment,
                              fileExists: (String) -> Bool = { FileManager.default.isExecutableFile(atPath: $0) }) -> String? {
        if let p = environment["ENGINEROOM_PATH"], fileExists(p) { return p }
        let home = environment["HOME"] ?? NSHomeDirectory()
        for dir in ["\(home)/.local/bin", "/opt/homebrew/bin", "/usr/local/bin", "\(home)/go/bin"] {
            let p = "\(dir)/engineroom"
            if fileExists(p) { return p }
        }
        return nil
    }

    /// Runs engineroom with args and returns its standard output and exit code. Each line of its
    /// standard error goes to onError as it arrives, when given.
    public func output(_ args: [String], onError: (@Sendable (Data) -> Void)? = nil) async throws -> (Data, Int32) {
        try await withCheckedThrowingContinuation { cont in
            let p = Process()
            p.executableURL = URL(fileURLWithPath: executable)
            p.arguments = args
            p.environment = environment
            // Both pipes are read while the program runs. Read only once it had exited, a reply
            // larger than a pipe's buffer (64 KB) left engineroom blocked writing it and never exiting,
            // so the call never returned (the history did this once it passed 64 KB).
            let out = Pipe(), err = Pipe()
            p.standardOutput = out
            p.standardError = err
            let collected = Collected()
            let errLines = LineSplitter()
            // Each pipe is read while the program runs, and read to its end once it has ended,
            // under one lock with the reading, so a chunk being read as it ends is never handed on
            // after the rest (PipeReader).
            let outReader = PipeReader(out) { collected.append($0) }
            let errReader = PipeReader(err) { data in
                if let onError { for line in errLines.feed(data) { onError(line) } }
            }
            p.terminationHandler = { proc in
                outReader.finish()
                errReader.finish()
                // The last line may lack its newline.
                if let onError { for line in errLines.feed(Data([0x0A])) { onError(line) } }
                cont.resume(returning: (collected.data, proc.terminationStatus))
            }
            do { try p.run() } catch {
                outReader.finish()
                errReader.finish()
                cont.resume(throwing: error)
            }
        }
    }

    /// Runs `engineroom <args>` and decodes its answer; exit 3 is the program's own refusal, decoded
    /// as an APIError so its words reach the window.
    public func json<T: Decodable>(_ type: T.Type, _ args: [String]) async throws -> T {
        let (data, code) = try await output(args)
        return try Self.decode(type, data: data, code: code)
    }

    public static func decode<T: Decodable>(_ type: T.Type, data: Data, code: Int32) throws -> T {
        let dec = JSONDecoder()
        dec.dateDecodingStrategy = .custom { d in
            let s = try d.singleValueContainer().decode(String.self)
            let f = ISO8601DateFormatter()
            f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
            if let date = f.date(from: s) { return date }
            f.formatOptions = [.withInternetDateTime]
            if let date = f.date(from: s) { return date }
            return Date(timeIntervalSince1970: 0)
        }
        if code != 0, let e = try? dec.decode(APIError.self, from: data) { throw e }
        do {
            return try dec.decode(type, from: data)
        } catch {
            let text = String(data: data, encoding: .utf8) ?? ""
            throw APIError("engineroom answered something the app cannot read (exit \(code)): \(text.prefix(300))")
        }
    }

    public func status() async throws -> Status { try await json(Status.self, ["api", "status"]) }
    public func sessions() async throws -> SessionsReply { try await json(SessionsReply.self, ["api", "sessions"]) }
    public func repos() async throws -> ReposReply { try await json(ReposReply.self, ["api", "repos"]) }
    public func plan(_ action: [String]) async throws -> PlanReply { try await plan(action) { _ in } }

    /// The plan for an action, telling onProgress (on the main actor, in order) each line of
    /// progress engineroom writes to its standard error while it reads the machines the plan
    /// needs, so the waiting screen shows a meter rather than nothing.
    public func plan(_ action: [String], onProgress: @escaping @MainActor (PlanProgress) -> Void) async throws -> PlanReply {
        let (data, code) = try await output(["api", "plan"] + action) { line in
            guard let p = PlanProgress.parse(line) else { return }
            DispatchQueue.main.async { MainActor.assumeIsolated { onProgress(p) } }
        }
        return try Self.decode(PlanReply.self, data: data, code: code)
    }
    public func history() async throws -> [HistoryEntry] { try await json([HistoryEntry].self, ["history", "--json", "--limit", "60"]) }
    public func set(_ key: String, _ value: String) async throws -> Status { try await json(Status.self, ["api", "set", key, value]) }

    /// Starts engineroom as a long-running child of the app (the forwarder's watch), in the same
    /// environment as every other run. The caller owns it and terminates it.
    public func spawn(_ args: [String]) throws -> Process {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: executable)
        p.arguments = args
        p.environment = environment
        p.standardOutput = FileHandle.nullDevice
        p.standardError = FileHandle.nullDevice
        try p.run()
        return p
    }

    /// Runs an action, calling onEvent on the main actor for each event in the order engineroom printed
    /// them, and returns whether it finished ok. Events and the result go through the main queue,
    /// which keeps their order, so the outcome never arrives before the steps.
    public func run(_ action: [String], onEvent: @escaping @MainActor (RunEvent) -> Void) async -> Bool {
        await withCheckedContinuation { cont in
            let p = Process()
            p.executableURL = URL(fileURLWithPath: executable)
            p.arguments = ["api", "run"] + action
            p.environment = environment
            let out = Pipe()
            p.standardOutput = out
            p.standardError = Pipe()
            let lines = LineSplitter()
            let result = Outcome()
            let deliver: @Sendable (Data) -> Void = { data in
                for line in lines.feed(data) {
                    guard let e = try? JSONDecoder().decode(RunEvent.self, from: line) else { continue }
                    if e.event == "done" { result.set(e.ok ?? false) }
                    DispatchQueue.main.async { MainActor.assumeIsolated { onEvent(e) } }
                }
            }
            // Once the program has ended its output is read to the end, in order after anything
            // being read as it ended (PipeReader): every event is then on the main queue ahead of
            // the answer, so the caller sees them all first.
            let reader = PipeReader(out, deliver)
            p.terminationHandler = { _ in
                reader.finish()
                deliver(Data([0x0A]))
                DispatchQueue.main.async { cont.resume(returning: result.get()) }
            }
            do { try p.run() } catch {
                reader.finish()
                cont.resume(returning: false)
            }
        }
    }
}

/// Reads a pipe as data arrives, handing each chunk on, and once the program has ended (finish)
/// reads the rest to the end. The reading as data arrives and the final read share one lock, so a
/// chunk read just as the program ended is handed on before the rest, never after it, and nothing
/// is read twice.
final class PipeReader: @unchecked Sendable {
    private let lock = NSLock()
    private var finished = false
    private let handle: FileHandle
    private let chunk: @Sendable (Data) -> Void

    init(_ pipe: Pipe, _ chunk: @escaping @Sendable (Data) -> Void) {
        handle = pipe.fileHandleForReading
        self.chunk = chunk
        handle.readabilityHandler = { [weak self] h in
            guard let self else { return }
            self.lock.lock()
            defer { self.lock.unlock() }
            if self.finished { return }
            let data = h.availableData
            if !data.isEmpty { self.chunk(data) }
        }
    }

    /// Stops the reading as data arrives and hands on the rest of the pipe, to its end.
    func finish() {
        lock.lock()
        defer { lock.unlock() }
        if finished { return }
        finished = true
        handle.readabilityHandler = nil
        let rest = handle.readDataToEndOfFile()
        if !rest.isEmpty { chunk(rest) }
    }
}

/// Whether a run finished ok, set from the reading thread and read once it has ended.
final class Outcome: @unchecked Sendable {
    private var ok = false
    private let lock = NSLock()
    func set(_ v: Bool) { lock.lock(); ok = v; lock.unlock() }
    func get() -> Bool { lock.lock(); defer { lock.unlock() }; return ok }
}

/// Splits a byte stream into lines, keeping a partial last line for the next chunk.
public final class LineSplitter: @unchecked Sendable {
    private var pending = Data()
    private let lock = NSLock()
    public init() {}
    public func feed(_ data: Data) -> [Data] {
        lock.lock(); defer { lock.unlock() }
        pending.append(data)
        var out: [Data] = []
        while let nl = pending.firstIndex(of: 0x0A) {
            let line = pending.subdata(in: pending.startIndex..<nl)
            if !line.isEmpty { out.append(line) }
            pending.removeSubrange(pending.startIndex...nl)
        }
        return out
    }
}

/// Bytes gathered from a pipe as they arrive, from the pipe's own reading thread.
final class Collected: @unchecked Sendable {
    private let lock = NSLock()
    private var buffer = Data()
    func append(_ d: Data) { lock.lock(); buffer.append(d); lock.unlock() }
    var data: Data { lock.lock(); defer { lock.unlock() }; return buffer }
}
