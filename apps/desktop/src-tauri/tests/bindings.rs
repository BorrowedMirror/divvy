//! Fails if `web/src/bindings.ts` is stale. Regenerate with `cargo xtask gen`.
#[test]
fn bindings_up_to_date() {
    let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../web/src/bindings.ts");
    let tmp = std::env::temp_dir().join(format!("kalandra-bindings-{}.ts", std::process::id()));
    kalandra_desktop::export_bindings(&tmp).expect("export");
    let fresh = std::fs::read_to_string(&tmp).expect("read fresh");
    if std::env::var_os("KALANDRA_UPDATE_BINDINGS").is_some() {
        std::fs::write(&path, &fresh).expect("write bindings");
        return;
    }
    let current = std::fs::read_to_string(&path).unwrap_or_default();
    assert!(current == fresh, "web/src/bindings.ts is stale; run `cargo xtask gen`");
}
