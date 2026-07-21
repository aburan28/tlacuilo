//! Runs the TLC model checker (from tla2tools.jar) and parses its
//! machine-readable `-tool` output into typed results, including
//! counterexample traces.

use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::process::Command;
use std::time::Duration;

use crate::ast::{Expr, Module};
use crate::cfg::Config;
use crate::parser;
use crate::trace::{State, Trace};
use crate::value;

/// Message severities of TLC's tool-mode protocol.
pub const SEVERITY_ERROR: i32 = 1;
pub const SEVERITY_WARNING: i32 = 3;
pub const SEVERITY_STATE: i32 = 4;

/// Well-known TLC message codes (tlc2.output.EC).
pub const CODE_VERSION: i32 = 2262;
pub const CODE_INVARIANT_VIOLATED: i32 = 2110;
pub const CODE_TEMPORAL_VIOLATED: i32 = 2116;
pub const CODE_STATE_PRINT: i32 = 2217;
pub const CODE_STATE_LOOP_OR_STUTTER: i32 = 2218;
pub const CODE_PROGRESS: i32 = 2200;
pub const CODE_FINAL_STATS: i32 = 2199;
pub const CODE_SEARCH_DEPTH: i32 = 2194;

/// One framed tool-mode message:
/// `@!@!@STARTMSG <code>:<severity> @!@!@ body @!@!@ENDMSG <code> @!@!@`.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Message {
    pub code: i32,
    pub severity: i32,
    pub body: String,
}

/// Parses TLC -tool output into framed messages; text outside frames is
/// ignored.
pub fn parse_tool_output(out: &str) -> Vec<Message> {
    let mut msgs = Vec::new();
    let mut cur: Option<(i32, i32)> = None;
    let mut body = String::new();
    for line in out.lines() {
        let trimmed = line.trim_end();
        if let Some(rest) = trimmed.strip_prefix("@!@!@STARTMSG ") {
            if let Some(head) = rest.strip_suffix(" @!@!@") {
                if let Some((code, sev)) = head.split_once(':') {
                    if let (Ok(c), Ok(s)) = (code.parse(), sev.parse()) {
                        cur = Some((c, s));
                        body.clear();
                        continue;
                    }
                }
            }
        }
        if trimmed.starts_with("@!@!@ENDMSG ") && trimmed.ends_with(" @!@!@") {
            if let Some((code, severity)) = cur.take() {
                msgs.push(Message {
                    code,
                    severity,
                    body: body.trim_end_matches('\n').to_string(),
                });
                continue;
            }
        }
        if cur.is_some() {
            body.push_str(line);
            body.push('\n');
        }
    }
    msgs
}

/// Classifies the outcome of a TLC run, derived from its exit status.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Status {
    Unknown,
    Success,
    AssumptionViolation, // exit 10
    Deadlock,            // exit 11
    SafetyViolation,     // exit 12: invariant or action property
    LivenessViolation,   // exit 13: temporal property
    Failure,             // parse/config/system errors
}

impl std::fmt::Display for Status {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(match self {
            Status::Unknown => "unknown",
            Status::Success => "success",
            Status::AssumptionViolation => "assumption violation",
            Status::Deadlock => "deadlock",
            Status::SafetyViolation => "safety violation",
            Status::LivenessViolation => "liveness violation",
            Status::Failure => "failure",
        })
    }
}

/// Maps a TLC exit code to a Status (verified against TLC's
/// tlc2.output.EC.ExitStatus values).
pub fn status_from_exit_code(code: i32) -> Status {
    match code {
        0 => Status::Success,
        10 => Status::AssumptionViolation,
        11 => Status::Deadlock,
        12 => Status::SafetyViolation,
        13 => Status::LivenessViolation,
        c if c >= 150 => Status::Failure,
        _ => Status::Unknown,
    }
}

/// An error message reported by TLC.
#[derive(Clone, Debug)]
pub struct TlcError {
    pub code: i32,
    pub message: String,
}

/// The outcome of a TLC run.
#[derive(Clone, Debug, Default)]
pub struct RunResult {
    pub status: Option<Status>,
    pub exit_code: i32,
    pub duration: Option<Duration>,
    pub version: String,
    pub states_generated: i64,
    pub distinct_states: i64,
    pub queue_states: i64,
    pub depth: i64,
    pub errors: Vec<TlcError>,
    pub warnings: Vec<String>,
    /// The counterexample behavior, when TLC printed one.
    pub trace: Option<Trace>,
    pub messages: Vec<Message>,
}

impl RunResult {
    pub fn status(&self) -> Status {
        self.status.unwrap_or(Status::Unknown)
    }

    pub fn ok(&self) -> bool {
        self.status() == Status::Success
    }

    /// None for successful runs; a descriptive error otherwise.
    pub fn err(&self) -> Option<String> {
        if self.ok() {
            return None;
        }
        Some(match self.errors.first() {
            Some(e) => format!("tlc: {}: {}", self.status(), e.message),
            None => format!("tlc: {} (exit code {})", self.status(), self.exit_code),
        })
    }
}

/// Interprets a message stream and exit code as a RunResult.
pub fn new_result(msgs: Vec<Message>, exit_code: i32) -> RunResult {
    let mut r = RunResult {
        status: Some(status_from_exit_code(exit_code)),
        exit_code,
        ..Default::default()
    };
    for m in &msgs {
        match m.code {
            CODE_VERSION => r.version = m.body.trim().to_string(),
            CODE_PROGRESS => {
                if let Some((depth, sg, ds, q)) = parse_progress(&m.body) {
                    r.depth = depth;
                    r.states_generated = sg;
                    r.distinct_states = ds;
                    r.queue_states = q;
                }
            }
            CODE_FINAL_STATS => {
                if let Some((sg, ds, q)) = parse_final_stats(&m.body) {
                    r.states_generated = sg;
                    r.distinct_states = ds;
                    r.queue_states = q;
                }
            }
            CODE_SEARCH_DEPTH => {
                if let Some(d) = last_int(&m.body) {
                    r.depth = d;
                }
            }
            CODE_STATE_PRINT if m.severity == SEVERITY_STATE => match parse_trace_state(&m.body) {
                Ok(s) => r.trace.get_or_insert_with(Trace::new).add_state(s),
                Err(e) => r.warnings.push(format!("unparseable trace state: {e}")),
            },
            CODE_STATE_LOOP_OR_STUTTER => {
                let t = r.trace.get_or_insert_with(Trace::new);
                let b = m.body.trim();
                if let Some(rest) = b.split_once(": Back to state ").map(|(_, r)| r) {
                    if let Ok(n) = rest
                        .split_whitespace()
                        .next()
                        .unwrap_or("")
                        .parse::<usize>()
                    {
                        t.loop_to = Some(n.saturating_sub(1));
                    }
                } else if b.contains(": Stuttering") {
                    t.stuttering = true;
                }
            }
            _ if m.severity == SEVERITY_ERROR => r.errors.push(TlcError {
                code: m.code,
                message: m.body.trim().to_string(),
            }),
            _ if m.severity == SEVERITY_WARNING => r.warnings.push(m.body.trim().to_string()),
            _ => {}
        }
    }
    r.messages = msgs;
    r
}

fn digits_before(body: &str, marker: &str) -> Option<i64> {
    let idx = body.find(marker)?;
    let head = &body[..idx];
    let start = head
        .rfind(|c: char| !(c.is_ascii_digit() || c == ','))
        .map(|i| i + 1)
        .unwrap_or(0);
    head[start..].replace(',', "").parse().ok()
}

fn parse_progress(body: &str) -> Option<(i64, i64, i64, i64)> {
    let depth = body
        .strip_prefix("Progress(")?
        .split(')')
        .next()?
        .parse()
        .ok()?;
    let (sg, ds, q) = parse_final_stats(body)?;
    Some((depth, sg, ds, q))
}

fn parse_final_stats(body: &str) -> Option<(i64, i64, i64)> {
    let queue = digits_before(body, " states left on queue")
        .or_else(|| digits_before(body, " state left on queue"))?;
    Some((
        digits_before(body, " states generated")?,
        digits_before(body, " distinct states found")?,
        queue,
    ))
}

fn last_int(body: &str) -> Option<i64> {
    body.split(|c: char| !c.is_ascii_digit())
        .rfind(|s| !s.is_empty())?
        .parse()
        .ok()
}

/// Parses one state-print body:
/// `2: <Next line 6, col 9 ...>` followed by `/\ x = 1` lines.
fn parse_trace_state(body: &str) -> Result<State, String> {
    let body = body.trim();
    let (head, rest) = match body.split_once('\n') {
        Some((h, r)) => (h, r),
        None => (body, ""),
    };
    let head = head.trim();
    let (num, action) = head
        .split_once(": ")
        .ok_or_else(|| format!("no state header in {head:?}"))?;
    let index: usize = num
        .parse()
        .map_err(|_| format!("bad state number in {head:?}"))?;
    let action = action
        .trim()
        .trim_start_matches('<')
        .trim_end_matches('>')
        .to_string();
    let mut s = State {
        index,
        action,
        vars: BTreeMap::new(),
    };
    let rest = rest.trim();
    if rest.is_empty() {
        return Ok(s);
    }
    let e = parser::parse_expr(rest).map_err(|e| e.to_string())?;
    let assigns: Vec<&Expr> = match &e {
        Expr::Junction { op, items } if op == "/\\" => items.iter().collect(),
        other => vec![other],
    };
    for mut a in assigns {
        while let Expr::Paren(x) = a {
            a = x;
        }
        let Expr::Binary { op, l, r } = a else {
            return Err(format!("state line is not an assignment: {a}"));
        };
        if op != "=" {
            return Err(format!("state line is not an assignment: {a}"));
        }
        let Expr::Ident(name) = l.as_ref() else {
            return Err(format!("assignment target is not a variable: {l}"));
        };
        let v = value::from_expr(r).map_err(|e| format!("variable {name}: {e}"))?;
        s.vars.insert(name.clone(), v);
    }
    Ok(s)
}

// ------------------------------------------------------------- runner

/// A runner or I/O failure (verification verdicts are in RunResult).
#[derive(Debug)]
pub struct TlcRunError(pub String);

impl std::fmt::Display for TlcRunError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for TlcRunError {}

type RunnerResult<T> = std::result::Result<T, TlcRunError>;

/// Locates tla2tools.jar: `$TLA2TOOLS_JAR`, `./tla2tools.jar`,
/// `~/.tlacuilo/tla2tools.jar`, then common system java directories.
pub fn find_jar() -> Option<PathBuf> {
    let mut candidates: Vec<PathBuf> = Vec::new();
    if let Ok(p) = std::env::var("TLA2TOOLS_JAR") {
        if !p.is_empty() {
            candidates.push(PathBuf::from(p));
        }
    }
    candidates.push(PathBuf::from("tla2tools.jar"));
    if let Some(home) = std::env::var_os("HOME") {
        candidates.push(Path::new(&home).join(".tlacuilo/tla2tools.jar"));
    }
    candidates.push(PathBuf::from("/usr/local/share/java/tla2tools.jar"));
    candidates.push(PathBuf::from("/usr/share/java/tla2tools.jar"));
    candidates.into_iter().find(|c| c.is_file())
}

/// Configures a TLC run. The default locates the jar via [`find_jar`]
/// and runs breadth-first model checking with deadlock checking on.
#[derive(Clone, Debug, Default)]
pub struct Options {
    pub java_path: Option<PathBuf>,
    pub jar_path: Option<PathBuf>,
    pub java_opts: Vec<String>,
    /// The .cfg file; TLC defaults to `<spec>.cfg`.
    pub config_path: Option<PathBuf>,
    /// `-workers N`; negative means `auto` (needs a current TLC).
    pub workers: i32,
    /// Passes `-deadlock` (TLC's inverted flag: turns the check OFF).
    pub disable_deadlock_check: bool,
    pub metadir: Option<PathBuf>,
    pub extra_args: Vec<String>,
}

/// Model-checks the spec at `spec_path` (a .tla file). Errors only when
/// TLC could not be executed; check `RunResult::status` for the verdict.
pub fn run(spec_path: &Path, opts: &Options) -> RunnerResult<RunResult> {
    let jar = match &opts.jar_path {
        Some(p) => p.clone(),
        None => find_jar().ok_or(TlcRunError(
            "tla2tools.jar not found: set TLA2TOOLS_JAR or place it in the working directory"
                .into(),
        ))?,
    };
    let java = opts
        .java_path
        .clone()
        .unwrap_or_else(|| PathBuf::from("java"));
    let mut cmd = Command::new(&java);
    if opts.java_opts.is_empty() {
        cmd.arg("-XX:+UseParallelGC");
    } else {
        cmd.args(&opts.java_opts);
    }
    cmd.arg("-cp").arg(&jar).arg("tlc2.TLC").arg("-tool");
    if let Some(c) = &opts.config_path {
        cmd.arg("-config").arg(c);
    }
    if opts.workers < 0 {
        cmd.arg("-workers").arg("auto");
    } else if opts.workers > 1 {
        cmd.arg("-workers").arg(opts.workers.to_string());
    }
    if opts.disable_deadlock_check {
        cmd.arg("-deadlock");
    }
    if let Some(m) = &opts.metadir {
        cmd.arg("-metadir").arg(m);
    }
    cmd.args(&opts.extra_args);
    cmd.arg(
        spec_path
            .file_name()
            .ok_or(TlcRunError(format!("bad spec path {spec_path:?}")))?,
    );
    if let Some(dir) = spec_path.parent() {
        cmd.current_dir(dir);
    }
    let started = std::time::Instant::now();
    let out = cmd
        .output()
        .map_err(|e| TlcRunError(format!("starting TLC (is java installed?): {e}")))?;
    let mut text = String::from_utf8_lossy(&out.stdout).into_owned();
    text.push_str(&String::from_utf8_lossy(&out.stderr));
    let msgs = parse_tool_output(&text);
    let mut r = new_result(msgs, out.status.code().unwrap_or(-1));
    r.duration = Some(started.elapsed());
    Ok(r)
}

/// A self-contained model-checking task whose inputs live in memory;
/// [`check`] writes them to a temp directory and runs TLC there.
#[derive(Clone, Debug, Default)]
pub struct Job {
    /// The spec to check (alternatively provide `source`).
    pub module: Option<Module>,
    pub source: String,
    /// Overrides the module name used for the .tla file.
    pub module_name: String,
    pub config: Config,
    /// Additional module name -> source.
    pub aux_modules: Vec<(String, String)>,
}

/// Writes the job's spec and config to a temp directory, runs TLC, and
/// cleans up.
pub fn check(job: &Job, opts: &Options) -> RunnerResult<RunResult> {
    let src = match &job.module {
        Some(m) => crate::printer::module_string(m),
        None => job.source.clone(),
    };
    if src.is_empty() {
        return Err(TlcRunError("Job needs module or source".into()));
    }
    let name = if !job.module_name.is_empty() {
        job.module_name.clone()
    } else if let Some(m) = &job.module {
        m.name.clone()
    } else {
        parser::parse(&src)
            .map_err(|e| TlcRunError(format!("cannot determine module name: {e}")))?
            .name
    };
    job.config
        .validate()
        .map_err(|e| TlcRunError(e.to_string()))?;

    // Unique per call: tests run in parallel and may share module names.
    static SEQ: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
    let seq = SEQ.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    let dir = std::env::temp_dir().join(format!(
        "tlacuilo-tlc-{}-{}-{}",
        std::process::id(),
        seq,
        name
    ));
    let _ = std::fs::remove_dir_all(&dir);
    std::fs::create_dir_all(&dir).map_err(|e| TlcRunError(e.to_string()))?;
    let cleanup = TempDir(dir.clone());

    let spec_path = dir.join(format!("{name}.tla"));
    std::fs::write(&spec_path, &src).map_err(|e| TlcRunError(e.to_string()))?;
    let cfg_path = dir.join(format!("{name}.cfg"));
    std::fs::write(&cfg_path, job.config.format()).map_err(|e| TlcRunError(e.to_string()))?;
    for (aux, aux_src) in &job.aux_modules {
        std::fs::write(dir.join(format!("{aux}.tla")), aux_src)
            .map_err(|e| TlcRunError(e.to_string()))?;
    }
    let mut opts = opts.clone();
    if opts.config_path.is_none() {
        opts.config_path = Some(PathBuf::from(format!("{name}.cfg")));
    }
    if opts.metadir.is_none() {
        opts.metadir = Some(dir.join("states"));
    }
    let r = run(&spec_path, &opts);
    drop(cleanup);
    r
}

struct TempDir(PathBuf);

impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::value::Value;

    fn fixture(name: &str) -> String {
        let p = Path::new(env!("CARGO_MANIFEST_DIR"))
            .join("../tlc/testdata")
            .join(name);
        std::fs::read_to_string(&p).unwrap_or_else(|e| panic!("fixture {p:?}: {e}"))
    }

    #[test]
    fn success_fixture() {
        let msgs = parse_tool_output(&fixture("success.out"));
        assert!(!msgs.is_empty());
        let r = new_result(msgs, 0);
        assert!(r.ok(), "errors: {:?}", r.errors);
        assert_eq!(
            (r.states_generated, r.distinct_states, r.queue_states),
            (6, 5, 0)
        );
        assert_eq!(r.depth, 5);
        assert!(r.trace.is_none());
        assert!(!r.version.is_empty());
    }

    #[test]
    fn invariant_violation_fixture() {
        let r = new_result(parse_tool_output(&fixture("violation.out")), 12);
        assert_eq!(r.status(), Status::SafetyViolation);
        assert!(r.errors.iter().any(|e| e.code == CODE_INVARIANT_VIOLATED));
        let t = r.trace.as_ref().expect("trace");
        assert_eq!(t.states.len(), 4);
        assert_eq!(t.states[0].action, "Initial predicate");
        assert_eq!(t.states[3].vars["x"], Value::int(3));
        let Value::Tuple(hist) = &t.states[3].vars["hist"] else {
            panic!()
        };
        assert_eq!(hist.len(), 3);
        assert!(t.marshal_itf().is_ok());
    }

    #[test]
    fn deadlock_fixture() {
        let r = new_result(parse_tool_output(&fixture("deadlock.out")), 11);
        assert_eq!(r.status(), Status::Deadlock);
        let t = r.trace.as_ref().unwrap();
        assert_eq!(t.states.len(), 3);
        assert_eq!(t.states[2].vars["x"], Value::int(2));
    }

    #[test]
    fn liveness_fixture() {
        let r = new_result(parse_tool_output(&fixture("liveness.out")), 13);
        assert_eq!(r.status(), Status::LivenessViolation);
        let t = r.trace.as_ref().unwrap();
        assert_eq!(t.states.len(), 4);
        assert!(t.stuttering);
        assert!(r.errors.iter().any(|e| e.code == CODE_TEMPORAL_VIOLATED));
    }

    #[test]
    fn status_mapping() {
        for (code, want) in [
            (0, Status::Success),
            (10, Status::AssumptionViolation),
            (11, Status::Deadlock),
            (12, Status::SafetyViolation),
            (13, Status::LivenessViolation),
            (150, Status::Failure),
            (255, Status::Failure),
        ] {
            assert_eq!(status_from_exit_code(code), want, "exit {code}");
        }
    }

    #[test]
    fn single_var_state() {
        let s =
            parse_trace_state("3: <Next line 5, col 9 to line 5, col 27 of module Dead>\nx = 2\n")
                .unwrap();
        assert_eq!(s.index, 3);
        assert_eq!(s.vars["x"], Value::int(2));
    }
}
