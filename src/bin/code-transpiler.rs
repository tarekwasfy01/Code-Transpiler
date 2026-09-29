use code_transpiler::{backend_status, gui, languages, routes, transpile_file};
use std::{env, path::PathBuf};
fn usage() {
    eprintln!("code-transpiler languages | routes | status | gui [port] | transpile <source> <target> <input> <output>");
}
fn main() {
    let a: Vec<String> = env::args().collect();
    let r = match a.get(1).map(String::as_str) {
        Some("languages") => {
            for l in languages() {
                println!("{}\t{}", l.id, l.name)
            }
            Ok(())
        }
        Some("routes") => {
            for (s, t) in routes() {
                println!("{s} -> {t}")
            }
            Ok(())
        }
        Some("status") => {
            let (name, ok) = backend_status();
            println!("engine={name} native={ok} fallback=false");
            Ok(())
        }
        Some("gui") => {
            let p = a.get(2).and_then(|x| x.parse().ok()).unwrap_or(8765);
            match gui(p, true) {
                Ok(h) => {
                    println!("{}", h.url());
                    loop {
                        std::thread::park();
                    }
                }
                Err(e) => Err(e.to_string()),
            }
        }
        Some("transpile") if a.len() == 6 => transpile_file(
            PathBuf::from(&a[4]),
            PathBuf::from(&a[5]),
            Some(&a[2]),
            &a[3],
        )
        .map(|_| ())
        .map_err(|e| e.to_string()),
        _ => {
            usage();
            Err("invalid arguments".into())
        }
    };
    if let Err(e) = r {
        eprintln!("error: {e}");
        std::process::exit(1)
    }
}
