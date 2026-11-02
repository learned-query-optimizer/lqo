# job.sql (sql) against pg_stat_statements on the --dsn server (110 entries, 98 reads, 3 not reads, 9 on system catalogs only)

| Shape | Query | From the file | From pg_stat_statements | Same |
|---|---|---|---|---|
| `0466dcec` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | keep the default plan | keep the default plan | yes |
| `05ae6e9d` | `SELECT MIN(chn.name) AS voiced_char, MIN(n.name) AS voicing…` | "nested loop only", −29% | keep the default plan | no |
| `06df28fb` | `SELECT MIN(cn.name) AS producing_company, MIN(miidx.info) A…` | keep the default plan | "no hash join", −83% | no |
| `078dfa3f` | `SELECT MIN(mc.note) AS production_note, MIN(t.title) AS mov…` | "no parallel workers", −99% | keep the default plan | no |
| `0cc52afa` | `SELECT MIN(cn.name) AS company_name, MIN(lt.link) AS link_t…` | "row counts", −40% | "no merge join", −82% | no |
| `0cf295d8` | `SELECT MIN(mc.note) AS production_note, MIN(t.title) AS mov…` | "no parallel workers", −92% | keep the default plan | no |
| `0da43d7b` | `SELECT MIN(cn1.name) AS first_company, MIN(cn2.name) AS sec…` | "no memoize", −52% | "nested loop only", −41% | no |
| `0ea80b8e` | `SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A…` | keep the default plan | keep the default plan | yes |
| `11d2f26e` | `SELECT MIN(cn.name) AS company_name, MIN(lt.link) AS link_t…` | keep the default plan | "no merge join", −93% | no |
| `135e5bc8` | `SELECT MIN(chn.name) AS character_name, MIN(mi_idx.info) AS…` | keep the default plan | "no parallel workers", −28% | no |
| `188e45da` | `SELECT MIN(cn.name) AS producing_company, MIN(lt.link) AS l…` | "nested loop only", −24% | "nested loop only", −33% | yes |
| `2066cfd6` | `SELECT MIN(chn.name) AS character_name, MIN(mi_idx.info) AS…` | "no index scan", −97% | "no memoize", −24% | no |
| `2656274c` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | "row counts", −84% | keep the default plan | no |
| `27998f6b` | `SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto…` | keep the default plan | keep the default plan | yes |
| `2a882a48` | `SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v…` | keep the default plan | keep the default plan | yes |
| `2b1949c5` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | "no bitmap scan", −28% | "no memoize", −47% | no |
| `2ec85d5d` | `SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS kung…` | keep the default plan | keep the default plan | yes |
| `311123fe` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | "row counts", −32% | "no parallel workers", −29% | no |
| `3564c7a4` | `SELECT MIN(lt.link) AS link_type, MIN(t1.title) AS first_mo…` | keep the default plan | keep the default plan | yes |
| `376e1e8f` | `SELECT MIN(an1.name) AS actress_pseudonym, MIN(t.title) AS …` | keep the default plan | keep the default plan | yes |
| `376fabaf` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | keep the default plan | "no parallel workers", −28% | no |
| `39172b8c` | `SELECT MIN(chn.name) AS voiced_char_name, MIN(n.name) AS vo…` | keep the default plan | keep the default plan | yes |
| `39b4cff4` | `SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v…` | keep the default plan | "no parallel workers", −70% | no |
| `3d48869d` | `SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto…` | keep the default plan | keep the default plan | yes |
| `3f5cfdf1` | `SELECT MIN(kt.kind) AS movie_kind, MIN(t.title) AS complete…` | keep the default plan | keep the default plan | yes |
| `407c64ff` | `SELECT MIN(mi.info) AS release_date, MIN(t.title) AS youtub…` | keep the default plan | "no parallel workers", −29% | no |
| `43101882` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | keep the default plan | keep the default plan | yes |
| `442faf79` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | "row counts, second pass", −24% | "no parallel workers", −36% | no |
| `450fe5f6` | `SELECT MIN(mc.note) AS production_note, MIN(t.title) AS mov…` | "no hash join", −99% | keep the default plan | no |
| `45290f03` | `SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r…` | keep the default plan | keep the default plan | yes |
| `46b42be8` | `SELECT MIN(n.name) AS of_person, MIN(t.title) AS biography_…` | "row counts", −52% | keep the default plan | no |
| `46c94c7f` | `SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r…` | "row counts", −23% | keep the default plan | no |
| `4a1dc513` | `SELECT MIN(cn.name) AS producing_company, MIN(lt.link) AS l…` | "nested loop only", −28% | "nested loop only", −33% | yes |
| `4a51ac6c` | `SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS c…` | keep the default plan | keep the default plan | yes |
| `4e8b8a70` | `SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r…` | "no parallel workers", −38% | keep the default plan | no |
| `534b68fc` | `SELECT MIN(chn.name) AS character, MIN(t.title) AS movie_wi…` | "no nested loop", −55% | keep the default plan | no |
| `5671211f` | `SELECT MIN(a1.name) AS writer_pseudo_name, MIN(t.title) AS …` | keep the default plan | keep the default plan | yes |
| `5798935c` | `SELECT MIN(at.title) AS aka_title, MIN(t.title) AS internet…` | "no index scan", −24% | keep the default plan | no |
| `5cce4f36` | `SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A…` | keep the default plan | keep the default plan | yes |
| `5d5082e3` | `SELECT MIN(an.name) AS acress_pseudonym, MIN(t.title) AS ja…` | keep the default plan | keep the default plan | yes |
| `631a5812` | `SELECT MIN(chn.name) AS voiced_char, MIN(n.name) AS voicing…` | "no hash join", −22% | keep the default plan | no |
| `653a69e1` | `SELECT MIN(t.title) AS movie_title FROM keyword AS k, movie…` | keep the default plan | keep the default plan | yes |
| `6744415b` | `SELECT MIN(cn1.name) AS first_company, MIN(cn2.name) AS sec…` | "nested loop only", −38% | "nested loop only", −35% | yes |
| `6d17eb01` | `SELECT MIN(n.name) AS member_in_charnamed_american_movie, M…` | "row counts", −65% | keep the default plan | no |
| `6d1d5dbb` | `SELECT MIN(kt.kind) AS movie_kind, MIN(t.title) AS complete…` | "no hash join", −26% | keep the default plan | no |
| `6eab06ef` | `SELECT MIN(chn.name) AS character, MIN(t.title) AS russian_…` | "no hash join", −50% | "no parallel workers", −57% | no |
| `6ef26656` | `SELECT MIN(chn.name) AS voiced_char_name, MIN(n.name) AS vo…` | "no parallel workers", −32% | "no parallel workers", −34% | yes |
| `70b0406f` | `SELECT MIN(chn.name) AS uncredited_voiced_character, MIN(t.…` | "no hash join", −27% | "no parallel workers", −55% | no |
| `718b520c` | `SELECT MIN(cn.name) AS producing_company, MIN(miidx.info) A…` | "row counts", −73% | keep the default plan | no |
| `729ccb90` | `SELECT MIN(mi.info) AS budget, MIN(t.title) AS unsuccsessfu…` | "no parallel workers", −95% | "no parallel workers", −76% | yes |
| `73f27e86` | `SELECT MIN(n.name) AS member_in_charnamed_movie FROM cast_i…` | "row counts", −66% | keep the default plan | no |
| `7483ed43` | `SELECT MIN(chn.name) AS character_name, MIN(mi_idx.info) AS…` | "no bitmap scan", −97% | "no memoize", −23% | no |
| `8461866c` | `SELECT MIN(cn.name) AS from_company, MIN(mc.note) AS produc…` | "no parallel workers", −47% | "no merge join", −98% | no |
| `850e1a80` | `SELECT MIN(cn.name) AS from_company, MIN(mc.note) AS produc…` | "row counts", −92% | "no merge join", −97% | no |
| `85cf65fe` | `SELECT MIN(n.name) AS member_in_charnamed_movie FROM cast_i…` | "row counts", −53% | keep the default plan | no |
| `86f7e26b` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | keep the default plan | keep the default plan | yes |
| `8cf8918f` | `SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS jap_…` | "no memoize", −64% | keep the default plan | no |
| `8ed005c6` | `SELECT MIN(t.title) AS complete_downey_ironman_movie FROM c…` | keep the default plan | keep the default plan | yes |
| `8f30d4e5` | `SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A…` | "row counts", −68% | keep the default plan | no |
| `8f7394ec` | `SELECT MIN(t.title) AS typical_european_movie FROM company_…` | keep the default plan | keep the default plan | yes |
| `91a61e77` | `SELECT MIN(mi.info) AS release_date, MIN(miidx.info) AS rat…` | "row counts", −55% | "no hash join", −82% | no |
| `92588c28` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | "no nested loop", −28% | keep the default plan | no |
| `929552c1` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | keep the default plan | "no parallel workers", −21% | no |
| `a05671a4` | `SELECT MIN(mi.info) AS release_date, MIN(t.title) AS modern…` | keep the default plan | keep the default plan | yes |
| `a801be56` | `SELECT MIN(n.name) AS member_in_charnamed_movie, MIN(n.name…` | "no nested loop", −44% | keep the default plan | no |
| `ae99dc85` | `SELECT MIN(mc.note) AS production_note, MIN(t.title) AS mov…` | "no parallel workers", −94% | keep the default plan | no |
| `b26e54ae` | `SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r…` | keep the default plan | keep the default plan | yes |
| `b8a77e31` | `SELECT MIN(n.name) AS cast_member_name, MIN(pi.info) AS cas…` | "no index scan", −48% | keep the default plan | no |
| `bcfea86c` | `SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS movie_ti…` | keep the default plan | keep the default plan | yes |
| `c213f9bf` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | "no bitmap scan", −79% | "no parallel workers", −25% | no |
| `c335bb2c` | `SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo…` | keep the default plan | keep the default plan | yes |
| `c3eba7e4` | `SELECT MIN(cn.name) AS from_company, MIN(lt.link) AS movie_…` | "row counts", −44% | keep the default plan | no |
| `c3f27144` | `SELECT MIN(t.title) AS american_vhs_movie FROM company_type…` | keep the default plan | keep the default plan | yes |
| `c56fadd6` | `SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS north_eu…` | keep the default plan | keep the default plan | yes |
| `c862384f` | `SELECT MIN(cn.name) AS producing_company, MIN(lt.link) AS l…` | "nested loop only", −34% | "nested loop only", −33% | yes |
| `c936eca9` | `SELECT MIN(t.title) AS american_movie FROM company_type AS …` | keep the default plan | keep the default plan | yes |
| `c9d70fe1` | `SELECT MIN(n.name) AS cast_member, MIN(t.title) AS complete…` | keep the default plan | keep the default plan | yes |
| `cc66d5be` | `SELECT MIN(n.name) AS of_person, MIN(t.title) AS biography_…` | "row counts, second pass", −86% | keep the default plan | no |
| `ccc29962` | `SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS northern…` | keep the default plan | keep the default plan | yes |
| `d468dc0f` | `SELECT MIN(cn.name) AS from_company, MIN(lt.link) AS movie_…` | keep the default plan | keep the default plan | yes |
| `dd1b70c1` | `SELECT MIN(t.title) AS movie_title FROM company_name AS cn,…` | "row counts", −42% | keep the default plan | no |
| `dd477274` | `SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v…` | keep the default plan | keep the default plan | yes |
| `ddcd52ba` | `SELECT MIN(cn1.name) AS first_company, MIN(cn2.name) AS sec…` | "nested loop only", −33% | "nested loop only", −24% | yes |
| `e14ae77f` | `SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS western_…` | keep the default plan | keep the default plan | yes |
| `e27f8ea2` | `SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS voic…` | keep the default plan | keep the default plan | yes |
| `e4943b34` | `SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r…` | keep the default plan | keep the default plan | yes |
| `e5a4de69` | `SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r…` | keep the default plan | keep the default plan | yes |
| `e61f5cbd` | `SELECT MIN(t.title) AS complete_downey_ironman_movie FROM c…` | keep the default plan | keep the default plan | yes |
| `e80417f5` | `SELECT MIN(an1.name) AS costume_designer_pseudo, MIN(t.titl…` | keep the default plan | not in this run |  |
| `ef249dc4` | `SELECT MIN(mi.info) AS release_date, MIN(t.title) AS intern…` | keep the default plan | keep the default plan | yes |
| `fa4f3af1` | `SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS jap_…` | keep the default plan | keep the default plan | yes |
| `fbf68f9d` | `SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto…` | keep the default plan | keep the default plan | yes |
| `fc50c266` | `SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r…` | "no hash join", −25% | keep the default plan | no |
| `ff8164ee` | `SELECT MIN(chn.name) AS voiced_char, MIN(n.name) AS voicing…` | keep the default plan | keep the default plan | yes |

Shapes in both runs: 93, with the same verdict and hint: 47.
Suggestions: 46 from the file, 27 from pg_stat_statements.
