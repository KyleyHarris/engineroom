import Foundation

/// The app's feed (docs/decisions.md D-023): `engineroom api feed`, held open while the app runs.
/// It sends the sessions as one JSON line now, and one more each time anything in them changes, so
/// the app has no timer of its own and opens no new connection to the engine for each look. A line
/// the app writes to it asks it to look now, which the app does after an action so what the action
/// changed shows at once; closing its input ends it, on the engine as well.
public final class Feed: @unchecked Sendable {
    let process: Process
    let input: FileHandle
    /// When it was started, so the app can tell a feed that ended at once (an engine that cannot be
    /// reached, or one too old to have a feed) from one that ran and then lost its connection.
    public let began = Date()

    init(process: Process, input: FileHandle) {
        self.process = process
        self.input = input
    }

    public var isRunning: Bool { process.isRunning }

    /// Asks the feed to look again now. Nothing is written to a feed that has ended: its input
    /// would be a pipe nobody reads.
    public func look() {
        guard process.isRunning else { return }
        try? input.write(contentsOf: Data([0x0A]))
    }

    /// Ends the feed: closing its input is how it, and the engine's, know nobody reads any more; it
    /// is stopped outright only if it has not ended a moment later.
    public func stop() {
        try? input.close()
        let p = process
        DispatchQueue.global().asyncAfter(deadline: .now() + 2) { if p.isRunning { p.terminate() } }
    }

    /// What one line of the feed says.
    public enum Line: Sendable {
        /// The sessions as they are now.
        case sessions(SessionsReply)
        /// Why they could not be read (engineDown when the engine did not answer).
        case problem(APIError)
        /// Nothing: a keep-alive the feed sends after a quiet minute, so a connection that died
        /// without closing is noticed.
        case quiet
    }

    /// Reads one line of the feed. A line that is neither a list nor an error is nil: it is
    /// ignored rather than shown.
    public static func parse(_ line: Data) -> Line? {
        if line.allSatisfy({ $0 == 0x20 || $0 == 0x0D }) { return .quiet }
        if let reply = try? JSONDecoder().decode(SessionsReply.self, from: line) { return .sessions(reply) }
        if let e = try? JSONDecoder().decode(APIError.self, from: line) { return .problem(e) }
        return nil
    }
}

extension EngineRoomClient {
    /// Starts the feed. onLine is called on the main actor with each line, in the order sent;
    /// onEnd once it has ended, for whatever reason.
    public func follow(onLine: @escaping @MainActor (Feed.Line) -> Void, onEnd: @escaping @MainActor () -> Void) throws -> Feed {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: executable)
        p.arguments = ["api", "feed"]
        p.environment = environment
        let input = Pipe(), out = Pipe()
        p.standardInput = input
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        let lines = LineSplitter()
        let deliver: @Sendable (Data) -> Void = { data in
            for line in lines.feed(data) {
                guard let l = Feed.parse(line) else { continue }
                DispatchQueue.main.async { MainActor.assumeIsolated { onLine(l) } }
            }
        }
        out.fileHandleForReading.readabilityHandler = { h in deliver(h.availableData) }
        p.terminationHandler = { _ in
            out.fileHandleForReading.readabilityHandler = nil
            deliver(out.fileHandleForReading.readDataToEndOfFile())
            DispatchQueue.main.async { MainActor.assumeIsolated { onEnd() } }
        }
        try p.run()
        return Feed(process: p, input: input.fileHandleForWriting)
    }
}
