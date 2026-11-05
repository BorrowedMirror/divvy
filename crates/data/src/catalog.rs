use kalandra_core::{
    catalog::{BaseDef, Catalog, StatDef, StaticDef, UniqueDef},
    item::ModKind,
};

use crate::{Bundle, Manifest};

/// Indexed, query-ready game data implementing [`Catalog`].
#[derive(Debug, Clone)]
pub struct GameData {
    pub manifest: Manifest,
    bundle: Bundle,
}

impl GameData {
    pub fn from_bundle(manifest: Manifest, bundle: Bundle) -> Self {
        Self { manifest, bundle }
    }

    pub fn bundle(&self) -> &Bundle {
        &self.bundle
    }
}

impl Catalog for GameData {
    fn stats_by_template(&self, _template: &str, _kind: ModKind) -> Vec<(&StatDef, bool)> {
        todo!("workstream C")
    }
    fn base(&self, _name: &str) -> Option<&BaseDef> {
        todo!("workstream C")
    }
    fn base_in_magic_name(&self, _full_name: &str) -> Option<&BaseDef> {
        todo!("workstream C")
    }
    fn unique(&self, _name: &str) -> Option<&UniqueDef> {
        todo!("workstream C")
    }
    fn static_item(&self, _text: &str) -> Option<&StaticDef> {
        todo!("workstream C")
    }
}
