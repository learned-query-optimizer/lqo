# LQO advisor report

- **Server:** PostgreSQL 16.15 (Ubuntu 16.15-1.pgdg24.04+2), database imdb (primary, allowed with --allow-primary)
- **Workload:** pg_stat_statements on the --dsn server (110 entries, 98 reads, 3 not reads, 9 on system catalogs only): 980 statements, 93 shapes
- **Settings:** standard hints (13), 3 timed runs after a warm-up, up to 10 samples per shape, a hint must save 20%; row-count hints with pg_hint_plan
- **Estimate:** one pass of the tested statements takes 16,637 ms now, 13,319 ms with these hints (−20%)

## Faster with a hint (27 of 93 tested)

### `7483ed43` 64.5 ms → 49.5 ms (−23%)

```sql
SELECT MIN(chn.name) AS character_name,
       MIN(mi_idx.info) AS rating,
       MIN(t.title) AS complete_hero_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     char_name AS chn,
     cast_info AS ci,
     info_type AS it2,
     keyword AS k,
     kind_type AS kt,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cct1.kind = 'cast'
  AND cct2.kind LIKE 'cast'
  AND chn.name IS NOT NULL
  AND (chn.name LIKE '''Baby'' Brent'
       OR chn.name LIKE '''Baby'' Brent')
  AND it2.info = 'LD additional information'
  AND k.keyword IN ('13-hour-work-shift',
                    'lost-mother',
                    '1400s',
                    'lost-in-the-woods',
                    '1520s',
                    'loss-of-fortune',
                    '1550s',
                    'loon',
                    '1650s',
                    'looking-through-a-letterbox')
  AND kt.kind = 'episode'
  AND t.production_year > '2005'
  AND kt.id = t.kind_id
  AND t.id = mk.movie_id
  AND t.id = ci.movie_id
  AND t.id = cc.movie_id
  AND t.id = mi_idx.movie_id
  AND mk.movie_id = ci.movie_id
  AND mk.movie_id = cc.movie_id
  AND mk.movie_id = mi_idx.movie_id
  AND ci.movie_id = cc.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND cc.movie_id = mi_idx.movie_id
  AND chn.id = ci.person_role_id
  AND n.id = ci.person_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND it2.id = mi_idx.info_type_id
```

- **Hint:** no memoize, 1 call
- **Plan:** same joins and scans
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_memoize = off;
```

### `2066cfd6` 65.2 ms → 49.6 ms (−24%)

```sql
SELECT MIN(chn.name) AS character_name,
       MIN(mi_idx.info) AS rating,
       MIN(n.name) AS playing_actor,
       MIN(t.title) AS complete_hero_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     char_name AS chn,
     cast_info AS ci,
     info_type AS it2,
     keyword AS k,
     kind_type AS kt,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cct1.kind = 'cast'
  AND cct2.kind LIKE 'cast'
  AND chn.name IS NOT NULL
  AND (chn.name LIKE '#96Z506 Fiona Zonioni'
       OR chn.name LIKE '#96Z506 Fiona Zonioni')
  AND it2.info = 'LD additional information'
  AND k.keyword IN ('13th-century',
                    'mandibular-tusk',
                    '1604-supernova',
                    'manchu',
                    '1790s',
                    'manchester-united',
                    '2030s',
                    'man-wearing-tidy-whities',
                    '21-year-old',
                    'man-walking-with-a-pram')
  AND kt.kind = 'episode'
  AND mi_idx.info > '3.7'
  AND t.production_year > '2004'
  AND kt.id = t.kind_id
  AND t.id = mk.movie_id
  AND t.id = ci.movie_id
  AND t.id = cc.movie_id
  AND t.id = mi_idx.movie_id
  AND mk.movie_id = ci.movie_id
  AND mk.movie_id = cc.movie_id
  AND mk.movie_id = mi_idx.movie_id
  AND ci.movie_id = cc.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND cc.movie_id = mi_idx.movie_id
  AND chn.id = ci.person_role_id
  AND n.id = ci.person_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND it2.id = mi_idx.info_type_id
```

- **Hint:** no memoize, 1 call
- **Plan:** same joins and scans
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_memoize = off;
```

### `c213f9bf` 77.8 ms → 58.2 ms (−25%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(n.name) AS writer,
       MIN(t.title) AS complete_violent_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     cast_info AS ci,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cct1.kind IN ('cast',
                    'crew')
  AND cct2.kind ='cast'
  AND ci.note IN ('(executive producer)',
                  '(voice: French Version)',
                  '(producer)',
                  '(voice) (rumored)',
                  '(production assistant)')
  AND it1.info = 'LD additional information'
  AND it2.info = 'LD additional information'
  AND k.keyword IN ('1000-pound-contact-bomb',
                    'methylamine',
                    '1360s',
                    'metal-fatigue',
                    '16th-amendment',
                    'merging-black-holes',
                    '1800s')
  AND mi.info IN ('English',
                  'Woodside Lodge, Welham Green, Hatfield, Hertfordshire, England, UK')
  AND n.gender = 'm'
  AND t.production_year > '2005'
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = cc.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = cc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = cc.movie_id
  AND mi_idx.movie_id = mk.movie_id
  AND mi_idx.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `39b4cff4` 745 ms → 224 ms (−70%)

```sql
SELECT MIN(an.name) AS alternative_name,
       MIN(chn.name) AS voiced_char_name,
       MIN(n.name) AS voicing_actress,
       MIN(t.title) AS american_movie
FROM aka_name AS an,
     char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     movie_companies AS mc,
     name AS n,
     role_type AS rt,
     title AS t
WHERE ci.note IN ('(producer)',
                  '(writers assistant)',
                  '(audience page)',
                  '(voice-over)')
  AND cn.country_code ='[us]'
  AND n.gender ='m'
  AND rt.role ='actor'
  AND ci.movie_id = t.id
  AND t.id = mc.movie_id
  AND ci.movie_id = mc.movie_id
  AND mc.company_id = cn.id
  AND ci.role_id = rt.id
  AND n.id = ci.person_id
  AND chn.id = ci.person_role_id
  AND an.person_id = n.id
  AND an.person_id = ci.person_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Hash Join, Seq Scan on movie_companies → Nested Loop, Index Scan on movie_companies
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `2b1949c5` 105 ms → 55.3 ms (−47%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(n.name) AS writer,
       MIN(t.title) AS complete_violent_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     cast_info AS ci,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cct1.kind = 'cast'
  AND cct2.kind ='cast'
  AND ci.note IN ('(voice)',
                  '(written by) (segment "A Friend of Dorothy''s")',
                  '(executive producer)',
                  '(voice: Japanese version)',
                  '(as Birgit Muller)')
  AND it1.info = 'LD additional information'
  AND it2.info = 'LD additional information'
  AND k.keyword IN ('.38-snubnose-revolver',
                    'lost-and-found',
                    '1060s',
                    'lost',
                    '13-year-old',
                    'loss-of-limb',
                    '13th-birthday')
  AND mi.info IN ('German',
                  'Zia Pueblo, New Mexico, USA',
                  'Dolby',
                  'Yugoslavia:102',
                  'PFM:35 mm',
                  'Yamaguchi, Japan')
  AND n.gender = 'm'
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = cc.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = cc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = cc.movie_id
  AND mi_idx.movie_id = mk.movie_id
  AND mi_idx.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
```

- **Hint:** no memoize, 1 call
- **Plan:** same joins and scans
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_memoize = off;
```

### `06df28fb` 103 ms → 17.4 ms (−83%)

```sql
SELECT MIN(cn.name) AS producing_company,
       MIN(miidx.info) AS rating,
       MIN(t.title) AS movie
FROM company_name AS cn,
     company_type AS ct,
     info_type AS it,
     info_type AS it2,
     kind_type AS kt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS miidx,
     title AS t
WHERE cn.country_code ='[us]'
  AND ct.kind ='distributors'
  AND it.info ='LD additional information'
  AND it2.info ='LD additional information'
  AND kt.kind ='episode'
  AND mi.movie_id = t.id
  AND it2.id = mi.info_type_id
  AND kt.id = t.kind_id
  AND mc.movie_id = t.id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
  AND miidx.movie_id = t.id
  AND it.id = miidx.info_type_id
  AND mi.movie_id = miidx.movie_id
  AND mi.movie_id = mc.movie_id
  AND miidx.movie_id = mc.movie_id
```

- **Hint:** no hash join, 1 call
- **Plan:** Hash Join ×4, Seq Scan on movie_info_idx, Seq Scan on info_type → Nested Loop ×4, Index Scan on info_type, Bitmap Heap Scan on movie_info_idx
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_hashjoin = off;
```

### `91a61e77` 99.3 ms → 17.6 ms (−82%)

```sql
SELECT MIN(mi.info) AS release_date,
       MIN(miidx.info) AS rating,
       MIN(t.title) AS german_movie
FROM company_name AS cn,
     company_type AS ct,
     info_type AS it,
     info_type AS it2,
     kind_type AS kt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS miidx,
     title AS t
WHERE cn.country_code ='[us]'
  AND ct.kind ='distributors'
  AND it.info ='LD additional information'
  AND it2.info ='LD additional information'
  AND kt.kind ='episode'
  AND mi.movie_id = t.id
  AND it2.id = mi.info_type_id
  AND kt.id = t.kind_id
  AND mc.movie_id = t.id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
  AND miidx.movie_id = t.id
  AND it.id = miidx.info_type_id
  AND mi.movie_id = miidx.movie_id
  AND mi.movie_id = mc.movie_id
  AND miidx.movie_id = mc.movie_id
```

- **Hint:** no hash join, 1 call
- **Plan:** Hash Join ×4, Seq Scan on movie_info_idx, Seq Scan on info_type → Nested Loop ×4, Index Scan on info_type, Bitmap Heap Scan on movie_info_idx
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_hashjoin = off;
```

### `850e1a80` 482 ms → 13.5 ms (−97%)

```sql
SELECT MIN(cn.name) AS from_company,
       MIN(mc.note) AS production_note,
       MIN(t.title) AS movie_based_on_book
FROM company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cn.country_code !='[ag]'
  AND (cn.name LIKE 'Bestia'
       OR cn.name LIKE 'Bestia')
  AND ct.kind != 'distributors'
  AND ct.kind IS NOT NULL
  AND k.keyword IN ('.22-magnum-rifle',
                    'malt-shop',
                    '100th-century-b.c.')
  AND mc.note IS NOT NULL
  AND t.production_year > '2005'
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
```

- **Hint:** no merge join, 1 call
- **Plan:** Merge Join, Memoize ×2, Index Scan on movie_keyword, Index Scan on company_type, Index Scan on keyword, Index Only Scan on link_type → Nested Loop, Seq Scan on keyword, Bitmap Heap Scan on movie_keyword, Seq Scan on company_type, Seq Scan on link_type
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_mergejoin = off;
```

### `70b0406f` 393 ms → 178 ms (−55%)

```sql
SELECT MIN(chn.name) AS uncredited_voiced_character,
       MIN(t.title) AS russian_movie
FROM char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     company_type AS ct,
     movie_companies AS mc,
     role_type AS rt,
     title AS t
WHERE ci.note LIKE '(writer)'
  AND ci.note LIKE '(writer)'
  AND cn.country_code = '[us]'
  AND rt.role = 'actor'
  AND t.production_year > '2004'
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND ci.movie_id = mc.movie_id
  AND chn.id = ci.person_role_id
  AND rt.id = ci.role_id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `929552c1` 71.5 ms → 56.3 ms (−21%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(n.name) AS writer,
       MIN(t.title) AS complete_gore_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     cast_info AS ci,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cct1.kind IN ('cast',
                    'crew')
  AND cct2.kind ='cast'
  AND ci.note IN ('(producer)',
                  '(voice) (archive footage) (uncredited)',
                  '(voice)',
                  '(translation)',
                  '(writer)')
  AND it1.info = 'LD additional information'
  AND it2.info = 'LD additional information'
  AND k.keyword IN ('10th-century-b.c.',
                    'm-901-improved-tow-vehicle',
                    '12-year-old',
                    'lying-to-employer',
                    '1760s',
                    'lying-on-bathroom-floor',
                    '2-of-spades')
  AND mi.info IN ('English',
                  'Zurawlów, Zamosc County, Lublin Voivodeship, Poland')
  AND n.gender = 'm'
  AND t.production_year > '2005'
  AND (t.title LIKE '(#1.1)'
       OR t.title LIKE '(#1.1)'
       OR t.title LIKE '(#1.1)')
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = cc.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = cc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = cc.movie_id
  AND mi_idx.movie_id = mk.movie_id
  AND mi_idx.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `442faf79` 71.3 ms → 45.9 ms (−36%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(n.name) AS writer,
       MIN(t.title) AS violent_liongate_movie
FROM cast_info AS ci,
     company_name AS cn,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE ci.note IN ('(writer)',
                  '(written by) (segment "Help Wanted")',
                  '(production assistant)',
                  '(written by) (segment "Dog Gone")',
                  '(written by)')
  AND cn.name LIKE 'BMG Video'
  AND it1.info = 'LD additional information'
  AND it2.info = 'LD additional information'
  AND k.keyword IN ('102nd-century',
                    'maintenance',
                    '1610s',
                    'mainstream',
                    '1820s',
                    'maiden-voyage',
                    '19th-amendment')
  AND mi.info IN ('English',
                  'Zoom Erlebniswelt Gelsenkirchen, Gelsenkirchen, North Rhine - Westphalia, Germany',
                  'PFM:35 mm',
                  'Zoo, Leipzig, Saxony, Germany',
                  'OFM:35 mm',
                  'Zoo Duisburg, Duisburg, North Rhine - Westphalia, Germany')
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = mc.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = mc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = mc.movie_id
  AND mi_idx.movie_id = mk.movie_id
  AND mi_idx.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND k.id = mk.keyword_id
  AND cn.id = mc.company_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Memoize, Index Scan on info_type ×2 → Seq Scan on info_type ×2
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `6eab06ef` 430 ms → 184 ms (−57%)

```sql
SELECT MIN(chn.name) AS character,
       MIN(t.title) AS russian_mov_with_actor_producer
FROM char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     company_type AS ct,
     movie_companies AS mc,
     role_type AS rt,
     title AS t
WHERE ci.note LIKE '(production assistant)'
  AND cn.country_code = '[us]'
  AND rt.role = 'actor'
  AND t.production_year > '2005'
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND ci.movie_id = mc.movie_id
  AND chn.id = ci.person_role_id
  AND rt.id = ci.role_id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `376fabaf` 72.1 ms → 51.9 ms (−28%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(n.name) AS writer,
       MIN(t.title) AS violent_liongate_movie
FROM cast_info AS ci,
     company_name AS cn,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE ci.note IN ('(voice)',
                  '(voices)',
                  '(uncredited)',
                  '(voice: English version) (uncredited)',
                  '(production assistant)')
  AND cn.name LIKE 'Nickelodeon'
  AND it1.info = 'LD additional information'
  AND it2.info = 'LD additional information'
  AND k.keyword IN ('100th-anniversary',
                    'lockheed-u-2-dragon-lady',
                    '1600s',
                    'locked-out-of-car',
                    '16mm-film',
                    'lock-in',
                    '17-year-old')
  AND mi.info IN ('RAT:1.33 : 1',
                  'USA:1980')
  AND n.gender = 'm'
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = mc.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = mc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = mc.movie_id
  AND mi_idx.movie_id = mk.movie_id
  AND mi_idx.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND k.id = mk.keyword_id
  AND cn.id = mc.company_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `8461866c` 864 ms → 13.7 ms (−98%)

```sql
SELECT MIN(cn.name) AS from_company,
       MIN(mc.note) AS production_note,
       MIN(t.title) AS movie_based_on_book
FROM company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cn.country_code !='[us]'
  AND ct.kind != 'distributors'
  AND ct.kind IS NOT NULL
  AND k.keyword IN ('.50-calibre-bullet',
                    'magic-lamp',
                    '11-year-old')
  AND mc.note IS NOT NULL
  AND t.production_year > '2004'
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
```

- **Hint:** no merge join, 1 call
- **Plan:** Hash Join, Merge Join, Index Scan on movie_keyword, Memoize ×2, Index Scan on keyword, Index Scan on company_type → Nested Loop ×2, Seq Scan on keyword, Bitmap Heap Scan on movie_keyword, Seq Scan on company_type
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_mergejoin = off;
```

### `135e5bc8` 65.5 ms → 47.2 ms (−28%)

```sql
SELECT MIN(chn.name) AS character_name,
       MIN(mi_idx.info) AS rating,
       MIN(t.title) AS complete_hero_movie
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     char_name AS chn,
     cast_info AS ci,
     info_type AS it2,
     keyword AS k,
     kind_type AS kt,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE cct1.kind = 'cast'
  AND cct2.kind LIKE 'cast'
  AND chn.name IS NOT NULL
  AND (chn.name LIKE '#8 Engineer'
       OR chn.name LIKE '#8 Engineer')
  AND it2.info = 'LD additional information'
  AND k.keyword IN ('12-hour-time-sequence',
                    'local-government',
                    '125th-street-manhattan-new-york-city',
                    'living-totem')
  AND kt.kind = 'episode'
  AND mi_idx.info > '3.8'
  AND t.production_year > '2004'
  AND kt.id = t.kind_id
  AND t.id = mk.movie_id
  AND t.id = ci.movie_id
  AND t.id = cc.movie_id
  AND t.id = mi_idx.movie_id
  AND mk.movie_id = ci.movie_id
  AND mk.movie_id = cc.movie_id
  AND mk.movie_id = mi_idx.movie_id
  AND ci.movie_id = cc.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND cc.movie_id = mi_idx.movie_id
  AND chn.id = ci.person_role_id
  AND n.id = ci.person_id
  AND k.id = mk.keyword_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND it2.id = mi_idx.info_type_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** same joins and scans
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `11d2f26e` 201 ms → 15.0 ms (−93%)

```sql
SELECT MIN(cn.name) AS company_name,
       MIN(lt.link) AS link_type,
       MIN(t.title) AS western_follow_up
FROM company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cn.country_code !='[ae]'
  AND (cn.name LIKE 'Abu Dhabi Television'
       OR cn.name LIKE 'Abu Dhabi Television')
  AND ct.kind ='distributors'
  AND k.keyword ='.hack'
  AND lt.link LIKE 'alternate language version of'
  AND mc.note IS NULL
  AND mi.info IN ('English',
                  'USA:31 January 2013',
                  'OFM:35 mm',
                  'USA:31 January 2002',
                  'RAT:1.33 : 1',
                  'USA:31 August 2008',
                  'PFM:35 mm',
                  'USA:30 September 2010')
  AND t.production_year BETWEEN '1994' AND '2009'
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND mi.movie_id = t.id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND ml.movie_id = mi.movie_id
  AND mk.movie_id = mi.movie_id
  AND mc.movie_id = mi.movie_id
```

- **Hint:** no merge join, 2 calls
- **Plan:** Merge Join, Memoize ×2, Index Scan on movie_keyword, Bitmap Heap Scan on movie_link, Index Scan on keyword, Index Scan on company_type → Nested Loop, Seq Scan on keyword, Bitmap Heap Scan on movie_keyword, Index Scan on movie_link, Seq Scan on company_type
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_mergejoin = off;
```

### `311123fe` 62.7 ms → 44.3 ms (−29%)

```sql
SELECT MIN(mi.info) AS movie_budget,
       MIN(mi_idx.info) AS movie_votes,
       MIN(n.name) AS writer,
       MIN(t.title) AS violent_liongate_movie
FROM cast_info AS ci,
     company_name AS cn,
     info_type AS it1,
     info_type AS it2,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     movie_keyword AS mk,
     name AS n,
     title AS t
WHERE ci.note IN ('(producer)',
                  '(voice) (uncredited)',
                  '(executive producer)',
                  '(videotape supervision)',
                  '(digital content officer)')
  AND cn.name LIKE 'Cartoon Network'
  AND it1.info = 'LD additional information'
  AND it2.info = 'LD additional information'
  AND k.keyword IN ('.45-calibre-pistol',
                    'logger',
                    '1000-pound-contact-bomb',
                    'lobotomy',
                    '165-degrees-farenheit',
                    'loaning-money',
                    '2-dimensional')
  AND mc.note LIKE '(2011) (USA) (TV)'
  AND mi.info IN ('PFM:35 mm',
                  'Providence, Rhode Island, USA')
  AND n.gender = 'm'
  AND t.production_year > '2005'
  AND (t.title LIKE '(#1.1)'
       OR t.title LIKE '(#1.1)'
       OR t.title LIKE '(#1.1)')
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND t.id = mc.movie_id
  AND ci.movie_id = mi.movie_id
  AND ci.movie_id = mi_idx.movie_id
  AND ci.movie_id = mk.movie_id
  AND ci.movie_id = mc.movie_id
  AND mi.movie_id = mi_idx.movie_id
  AND mi.movie_id = mk.movie_id
  AND mi.movie_id = mc.movie_id
  AND mi_idx.movie_id = mk.movie_id
  AND mi_idx.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND n.id = ci.person_id
  AND it1.id = mi.info_type_id
  AND it2.id = mi_idx.info_type_id
  AND k.id = mk.keyword_id
  AND cn.id = mc.company_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Index Scan on info_type ×2 → Seq Scan on info_type ×2
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `0da43d7b` 105 ms → 62.3 ms (−41%)

```sql
SELECT MIN(cn1.name) AS first_company,
       MIN(cn2.name) AS second_company,
       MIN(mi_idx1.info) AS first_rating,
       MIN(mi_idx2.info) AS second_rating,
       MIN(t1.title) AS first_movie,
       MIN(t2.title) AS second_movie
FROM company_name AS cn1,
     company_name AS cn2,
     info_type AS it1,
     info_type AS it2,
     kind_type AS kt1,
     kind_type AS kt2,
     link_type AS lt,
     movie_companies AS mc1,
     movie_companies AS mc2,
     movie_info_idx AS mi_idx1,
     movie_info_idx AS mi_idx2,
     movie_link AS ml,
     title AS t1,
     title AS t2
WHERE cn1.country_code != '[us]'
  AND it1.info = 'LD additional information'
  AND it2.info = 'LD additional information'
  AND kt1.kind IN ('episode',
                   'video movie')
  AND kt2.kind IN ('episode',
                   'video movie')
  AND lt.link IN ('alternate language version of',
                  'version of',
                  'edited from')
  AND mi_idx2.info < '33'
  AND t2.production_year BETWEEN '1995' AND '2009'
  AND lt.id = ml.link_type_id
  AND t1.id = ml.movie_id
  AND t2.id = ml.linked_movie_id
  AND it1.id = mi_idx1.info_type_id
  AND t1.id = mi_idx1.movie_id
  AND kt1.id = t1.kind_id
  AND cn1.id = mc1.company_id
  AND t1.id = mc1.movie_id
  AND ml.movie_id = mi_idx1.movie_id
  AND ml.movie_id = mc1.movie_id
  AND mi_idx1.movie_id = mc1.movie_id
  AND it2.id = mi_idx2.info_type_id
  AND t2.id = mi_idx2.movie_id
  AND kt2.id = t2.kind_id
  AND cn2.id = mc2.company_id
  AND t2.id = mc2.movie_id
  AND ml.linked_movie_id = mi_idx2.movie_id
  AND ml.linked_movie_id = mc2.movie_id
  AND mi_idx2.movie_id = mc2.movie_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×2, Merge Join, Seq Scan on movie_link → Nested Loop ×3, Bitmap Heap Scan on movie_link, Memoize ×2
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `729ccb90` 44.9 ms → 10.7 ms (−76%)

```sql
SELECT MIN(mi.info) AS budget,
       MIN(t.title) AS unsuccsessful_movie
FROM company_name AS cn,
     company_type AS ct,
     info_type AS it1,
     info_type AS it2,
     movie_companies AS mc,
     movie_info AS mi,
     movie_info_idx AS mi_idx,
     title AS t
WHERE cn.country_code ='[us]'
  AND ct.kind IS NOT NULL
  AND (ct.kind ='distributors'
       OR ct.kind = 'distributors')
  AND it1.info ='LD additional information'
  AND it2.info ='LD additional information'
  AND t.production_year >'2004'
  AND (t.title LIKE '(#1.3)'
       OR t.title LIKE '(#1.3)')
  AND t.id = mi.movie_id
  AND t.id = mi_idx.movie_id
  AND mi.info_type_id = it1.id
  AND mi_idx.info_type_id = it2.id
  AND t.id = mc.movie_id
  AND ct.id = mc.company_type_id
  AND cn.id = mc.company_id
  AND mc.movie_id = mi.movie_id
  AND mc.movie_id = mi_idx.movie_id
  AND mi.movie_id = mi_idx.movie_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Hash Join, Seq Scan on movie_info_idx → Nested Loop, Bitmap Heap Scan on movie_info_idx
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `0cc52afa` 84.1 ms → 15.3 ms (−82%)

```sql
SELECT MIN(cn.name) AS company_name,
       MIN(lt.link) AS link_type,
       MIN(t.title) AS german_follow_up
FROM company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cn.country_code !='[ae]'
  AND (cn.name LIKE 'Optix Digital Pictures'
       OR cn.name LIKE 'Optix Digital Pictures')
  AND ct.kind ='distributors'
  AND k.keyword ='11-year-prison-sentence'
  AND lt.link LIKE 'alternate language version of'
  AND mc.note IS NULL
  AND mi.info IN ('English',
                  'Zaandam, Zaanstad, Noord-Holland, Netherlands')
  AND t.production_year BETWEEN '1996' AND '2009'
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND mi.movie_id = t.id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND ml.movie_id = mi.movie_id
  AND mk.movie_id = mi.movie_id
  AND mc.movie_id = mi.movie_id
```

- **Hint:** no merge join, 1 call
- **Plan:** Merge Join, Memoize, Index Scan on movie_keyword, Bitmap Heap Scan on movie_link, Index Scan on company_type, Index Scan on keyword → Nested Loop, Seq Scan on keyword, Bitmap Heap Scan on movie_keyword, Index Scan on movie_link, Seq Scan on company_type
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_mergejoin = off;
```

### `6ef26656` 78.2 ms → 51.7 ms (−34%)

```sql
SELECT MIN(chn.name) AS voiced_char_name,
       MIN(n.name) AS voicing_actress_name,
       MIN(t.title) AS kung_fu_panda
FROM aka_name AS an,
     char_name AS chn,
     cast_info AS ci,
     company_name AS cn,
     info_type AS it,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     name AS n,
     role_type AS rt,
     title AS t
WHERE ci.note IN ('(producer)',
                  '(writer) (as David J. Kratz)',
                  '(writer)',
                  '(uncredited) (archive footage)')
  AND cn.country_code ='[ae]'
  AND cn.name = 'Gamal El Leithy Films'
  AND it.info = 'LD additional information'
  AND k.keyword IN ('100th-century-b.c.',
                    'life-crisis',
                    '1100s',
                    'licking-dog')
  AND mi.info IS NOT NULL
  AND (mi.info LIKE 'English'
       OR mi.info LIKE 'English')
  AND n.gender ='m'
  AND n.name LIKE 'Anderson, Matthew'
  AND rt.role ='actor'
  AND t.production_year > '2005'
  AND t.title LIKE '(#1.2)'
  AND t.id = mi.movie_id
  AND t.id = mc.movie_id
  AND t.id = ci.movie_id
  AND t.id = mk.movie_id
  AND mc.movie_id = ci.movie_id
  AND mc.movie_id = mi.movie_id
  AND mc.movie_id = mk.movie_id
  AND mi.movie_id = ci.movie_id
  AND mi.movie_id = mk.movie_id
  AND ci.movie_id = mk.movie_id
  AND cn.id = mc.company_id
  AND it.id = mi.info_type_id
  AND n.id = ci.person_id
  AND rt.id = ci.role_id
  AND n.id = an.person_id
  AND ci.person_id = an.person_id
  AND chn.id = ci.person_role_id
  AND k.id = mk.keyword_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Index Scan on role_type, Index Scan on info_type → Seq Scan on info_type, Seq Scan on role_type
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `407c64ff` 29.2 ms → 20.7 ms (−29%)

```sql
SELECT MIN(mi.info) AS release_date,
       MIN(t.title) AS youtube_movie
FROM aka_title AS at,
     company_name AS cn,
     company_type AS ct,
     info_type AS it1,
     keyword AS k,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     title AS t
WHERE cn.country_code = '[ae]'
  AND cn.name = 'Al Sharqiya'
  AND it1.info = 'LD additional information'
  AND mc.note LIKE '(1997) (USA) (TV)'
  AND mc.note LIKE '(1997) (USA) (TV)'
  AND mi.note LIKE '(silent)'
  AND mi.info LIKE 'Italian'
  AND t.production_year BETWEEN '1994' AND '2009'
  AND t.id = at.movie_id
  AND t.id = mi.movie_id
  AND t.id = mk.movie_id
  AND t.id = mc.movie_id
  AND mk.movie_id = mi.movie_id
  AND mk.movie_id = mc.movie_id
  AND mk.movie_id = at.movie_id
  AND mi.movie_id = mc.movie_id
  AND mi.movie_id = at.movie_id
  AND mc.movie_id = at.movie_id
  AND k.id = mk.keyword_id
  AND it1.id = mi.info_type_id
  AND cn.id = mc.company_id
  AND ct.id = mc.company_type_id
```

- **Hint:** no parallel workers, 1 call
- **Plan:** Index Scan on info_type → Seq Scan on info_type
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL max_parallel_workers_per_gather = 0;
```

### `c862384f` 57.2 ms → 38.6 ms (−33%)

```sql
SELECT MIN(cn.name) AS producing_company,
       MIN(lt.link) AS link_type,
       MIN(t.title) AS complete_western_sequel
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cct1.kind = 'cast'
  AND cct2.kind LIKE 'cast'
  AND cn.country_code !='[ae]'
  AND (cn.name LIKE 'Almas Productions'
       OR cn.name LIKE 'Almas Productions')
  AND ct.kind ='distributors'
  AND k.keyword ='.22-calibre-bullet'
  AND lt.link LIKE 'alternate language version of'
  AND mc.note IS NULL
  AND mi.info IN ('English',
                  'USA:29 February 2008',
                  'OFM:35 mm',
                  'USA:28 March 2013',
                  'PFM:35 mm',
                  'USA:28 March 2005',
                  'PCS:Spherical',
                  'USA:28 January 2013',
                  'RAT:1.33 : 1')
  AND t.production_year BETWEEN '1996' AND '2009'
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND mi.movie_id = t.id
  AND t.id = cc.movie_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND ml.movie_id = mi.movie_id
  AND mk.movie_id = mi.movie_id
  AND mc.movie_id = mi.movie_id
  AND ml.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi.movie_id = cc.movie_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×3, Merge Join → Nested Loop ×4
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `ddcd52ba` 82.6 ms → 62.4 ms (−24%)

```sql
SELECT MIN(cn1.name) AS first_company,
       MIN(cn2.name) AS second_company,
       MIN(mi_idx1.info) AS first_rating,
       MIN(mi_idx2.info) AS second_rating,
       MIN(t1.title) AS first_movie,
       MIN(t2.title) AS second_movie
FROM company_name AS cn1,
     company_name AS cn2,
     info_type AS it1,
     info_type AS it2,
     kind_type AS kt1,
     kind_type AS kt2,
     link_type AS lt,
     movie_companies AS mc1,
     movie_companies AS mc2,
     movie_info_idx AS mi_idx1,
     movie_info_idx AS mi_idx2,
     movie_link AS ml,
     title AS t1,
     title AS t2
WHERE cn1.country_code = '[us]'
  AND it1.info = 'LD additional information'
  AND it2.info = 'LD additional information'
  AND kt1.kind IN ('episode')
  AND kt2.kind IN ('episode')
  AND lt.link IN ('alternate language version of',
                  'version of',
                  'edited from')
  AND mi_idx2.info < '266'
  AND t2.production_year BETWEEN '1996' AND '2010'
  AND lt.id = ml.link_type_id
  AND t1.id = ml.movie_id
  AND t2.id = ml.linked_movie_id
  AND it1.id = mi_idx1.info_type_id
  AND t1.id = mi_idx1.movie_id
  AND kt1.id = t1.kind_id
  AND cn1.id = mc1.company_id
  AND t1.id = mc1.movie_id
  AND ml.movie_id = mi_idx1.movie_id
  AND ml.movie_id = mc1.movie_id
  AND mi_idx1.movie_id = mc1.movie_id
  AND it2.id = mi_idx2.info_type_id
  AND t2.id = mi_idx2.movie_id
  AND kt2.id = t2.kind_id
  AND cn2.id = mc2.company_id
  AND t2.id = mc2.movie_id
  AND ml.linked_movie_id = mi_idx2.movie_id
  AND ml.linked_movie_id = mc2.movie_id
  AND mi_idx2.movie_id = mc2.movie_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×2, Merge Join, Seq Scan on movie_link, Index Scan on kind_type → Nested Loop ×3, Seq Scan on kind_type, Bitmap Heap Scan on movie_link, Memoize
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `188e45da` 56.6 ms → 38.0 ms (−33%)

```sql
SELECT MIN(cn.name) AS producing_company,
       MIN(lt.link) AS link_type,
       MIN(t.title) AS complete_western_sequel
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cct1.kind IN ('cast',
                    'crew')
  AND cct2.kind = 'cast'
  AND cn.country_code !='[ae]'
  AND (cn.name LIKE 'Hashmic House Films'
       OR cn.name LIKE 'Hashmic House Films')
  AND ct.kind ='distributors'
  AND k.keyword ='12-chapters'
  AND lt.link LIKE 'alternate language version of'
  AND mc.note IS NULL
  AND mi.info IN ('English',
                  'USA:5 November 1999',
                  'Portuguese',
                  'USA:5 March 1999')
  AND t.production_year BETWEEN '1995' AND '2009'
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND mi.movie_id = t.id
  AND t.id = cc.movie_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND ml.movie_id = mi.movie_id
  AND mk.movie_id = mi.movie_id
  AND mc.movie_id = mi.movie_id
  AND ml.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi.movie_id = cc.movie_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×3, Merge Join, Seq Scan on comp_cast_type → Nested Loop ×4, Memoize, Index Scan on comp_cast_type
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `6744415b` 75.4 ms → 49.0 ms (−35%)

```sql
SELECT MIN(cn1.name) AS first_company,
       MIN(cn2.name) AS second_company,
       MIN(mi_idx1.info) AS first_rating,
       MIN(mi_idx2.info) AS second_rating,
       MIN(t1.title) AS first_movie,
       MIN(t2.title) AS second_movie
FROM company_name AS cn1,
     company_name AS cn2,
     info_type AS it1,
     info_type AS it2,
     kind_type AS kt1,
     kind_type AS kt2,
     link_type AS lt,
     movie_companies AS mc1,
     movie_companies AS mc2,
     movie_info_idx AS mi_idx1,
     movie_info_idx AS mi_idx2,
     movie_link AS ml,
     title AS t1,
     title AS t2
WHERE cn1.country_code = '[us]'
  AND it1.info = 'LD additional information'
  AND it2.info = 'LD additional information'
  AND kt1.kind IN ('episode')
  AND kt2.kind IN ('episode')
  AND lt.link LIKE 'alternate language version of'
  AND mi_idx2.info < '3.9'
  AND t2.production_year = '2011'
  AND lt.id = ml.link_type_id
  AND t1.id = ml.movie_id
  AND t2.id = ml.linked_movie_id
  AND it1.id = mi_idx1.info_type_id
  AND t1.id = mi_idx1.movie_id
  AND kt1.id = t1.kind_id
  AND cn1.id = mc1.company_id
  AND t1.id = mc1.movie_id
  AND ml.movie_id = mi_idx1.movie_id
  AND ml.movie_id = mc1.movie_id
  AND mi_idx1.movie_id = mc1.movie_id
  AND it2.id = mi_idx2.info_type_id
  AND t2.id = mi_idx2.movie_id
  AND kt2.id = t2.kind_id
  AND cn2.id = mc2.company_id
  AND t2.id = mc2.movie_id
  AND ml.linked_movie_id = mi_idx2.movie_id
  AND ml.linked_movie_id = mc2.movie_id
  AND mi_idx2.movie_id = mc2.movie_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×2, Merge Join → Nested Loop ×3
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

### `4a1dc513` 55.3 ms → 37.3 ms (−33%)

```sql
SELECT MIN(cn.name) AS producing_company,
       MIN(lt.link) AS link_type,
       MIN(t.title) AS complete_western_sequel
FROM complete_cast AS cc,
     comp_cast_type AS cct1,
     comp_cast_type AS cct2,
     company_name AS cn,
     company_type AS ct,
     keyword AS k,
     link_type AS lt,
     movie_companies AS mc,
     movie_info AS mi,
     movie_keyword AS mk,
     movie_link AS ml,
     title AS t
WHERE cct1.kind IN ('cast',
                    'crew')
  AND cct2.kind = 'cast'
  AND cn.country_code !='[ae]'
  AND (cn.name LIKE 'Oedipuss Films'
       OR cn.name LIKE 'Oedipuss Films')
  AND ct.kind ='distributors'
  AND k.keyword ='100th-episode'
  AND lt.link LIKE 'alternate language version of'
  AND mc.note IS NULL
  AND mi.info IN ('English',
                  'USA:18 October 1978',
                  'PFM:35 mm',
                  'USA:18 November 2006')
  AND t.production_year = '2011'
  AND lt.id = ml.link_type_id
  AND ml.movie_id = t.id
  AND t.id = mk.movie_id
  AND mk.keyword_id = k.id
  AND t.id = mc.movie_id
  AND mc.company_type_id = ct.id
  AND mc.company_id = cn.id
  AND mi.movie_id = t.id
  AND t.id = cc.movie_id
  AND cct1.id = cc.subject_id
  AND cct2.id = cc.status_id
  AND ml.movie_id = mk.movie_id
  AND ml.movie_id = mc.movie_id
  AND mk.movie_id = mc.movie_id
  AND ml.movie_id = mi.movie_id
  AND mk.movie_id = mi.movie_id
  AND mc.movie_id = mi.movie_id
  AND ml.movie_id = cc.movie_id
  AND mk.movie_id = cc.movie_id
  AND mc.movie_id = cc.movie_id
  AND mi.movie_id = cc.movie_id
```

- **Hint:** nested loop only, 1 call
- **Plan:** Hash Join ×3, Merge Join, Seq Scan on comp_cast_type → Nested Loop ×4, Memoize, Index Scan on comp_cast_type
- **Note:** pg_stat_statements keeps no values, so LQO took the values for its parameters from rows of the tables they're compared with

```sql
SET LOCAL enable_hashjoin = off;
SET LOCAL enable_mergejoin = off;
```

## Keep the default plan (66)

| Shape | Query | Time | Why |
|---|---|---|---|
| `8f30d4e5` | SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A… | 11.7 ms | no other plan was clearly faster |
| `73f27e86` | SELECT MIN(n.name) AS member_in_charnamed_movie FROM cast_i… | 11.6 ms | no other plan was clearly faster |
| `a801be56` | SELECT MIN(n.name) AS member_in_charnamed_movie, MIN(n.name… | 10.9 ms | no other plan was clearly faster |
| `8cf8918f` | SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS jap_… | 637 ms | a hint helped one sample but was slower on another |
| `6d17eb01` | SELECT MIN(n.name) AS member_in_charnamed_american_movie, M… | 14.8 ms | no other plan was clearly faster |
| `85cf65fe` | SELECT MIN(n.name) AS member_in_charnamed_movie FROM cast_i… | 10.5 ms | no other plan was clearly faster |
| `534b68fc` | SELECT MIN(chn.name) AS character, MIN(t.title) AS movie_wi… | 720 ms | a hint helped one sample but was slower on another |
| `92588c28` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 50.8 ms | no other plan was clearly faster |
| `5671211f` | SELECT MIN(a1.name) AS writer_pseudo_name, MIN(t.title) AS … | 2,347 ms | a hint helped one sample but was slower on another |
| `27998f6b` | SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto… | 36.3 ms | no other plan was clearly faster |
| `3d48869d` | SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto… | 29.8 ms | no other plan was clearly faster |
| `86f7e26b` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 316 ms | a hint helped one sample but was slower on another |
| `2656274c` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 901 ms | a hint helped one sample but was slower on another |
| `b8a77e31` | SELECT MIN(n.name) AS cast_member_name, MIN(pi.info) AS cas… | 51.5 ms | a hint helped one sample but was slower on another |
| `c335bb2c` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 26.8 ms | no other plan was clearly faster |
| `8ed005c6` | SELECT MIN(t.title) AS complete_downey_ironman_movie FROM c… | 28.9 ms | no other plan was clearly faster |
| `dd1b70c1` | SELECT MIN(t.title) AS movie_title FROM company_name AS cn,… | 7.81 ms | no other plan was clearly faster |
| `e5a4de69` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 46.7 ms | no other plan was clearly faster |
| `e61f5cbd` | SELECT MIN(t.title) AS complete_downey_ironman_movie FROM c… | 27.1 ms | no other plan was clearly faster |
| `718b520c` | SELECT MIN(cn.name) AS producing_company, MIN(miidx.info) A… | 140 ms | a hint helped one sample but was slower on another |
| `5cce4f36` | SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A… | 10.9 ms | no other plan was clearly faster |
| `0ea80b8e` | SELECT MIN(an.name) AS cool_actor_pseudonym, MIN(t.title) A… | 12.5 ms | no other plan was clearly faster |
| `376e1e8f` | SELECT MIN(an1.name) AS actress_pseudonym, MIN(t.title) AS … | 57.6 ms | a hint helped one sample but was slower on another |
| `fc50c266` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 123 ms | a hint helped one sample but was slower on another |
| `c9d70fe1` | SELECT MIN(n.name) AS cast_member, MIN(t.title) AS complete… | 22.1 ms | no other plan was clearly faster |
| `cc66d5be` | SELECT MIN(n.name) AS of_person, MIN(t.title) AS biography_… | 49.9 ms | a hint helped one sample but was slower on another |
| `6d1d5dbb` | SELECT MIN(kt.kind) AS movie_kind, MIN(t.title) AS complete… | 100 ms | a hint helped one sample but was slower on another |
| `45290f03` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 55.0 ms | no other plan was clearly faster |
| `5798935c` | SELECT MIN(at.title) AS aka_title, MIN(t.title) AS internet… | 233 ms | a hint helped one sample but was slower on another |
| `dd477274` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v… | 109 ms | a hint helped one sample but was slower on another |
| `b26e54ae` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 79.2 ms | no other plan was clearly faster |
| `fa4f3af1` | SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS jap_… | 116 ms | a hint helped one sample but was slower on another |
| `4e8b8a70` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 78.7 ms | no other plan was clearly faster |
| `653a69e1` | SELECT MIN(t.title) AS movie_title FROM keyword AS k, movie… | 7.09 ms | no other plan was clearly faster |
| `a05671a4` | SELECT MIN(mi.info) AS release_date, MIN(t.title) AS modern… | 200 ms | a hint helped one sample but was slower on another |
| `c56fadd6` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS north_eu… | 20.1 ms | no other plan was clearly faster |
| `e27f8ea2` | SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS voic… | 95.2 ms | a hint helped one sample but was slower on another |
| `4a51ac6c` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS c… | 60.6 ms | a hint helped one sample but was slower on another |
| `ef249dc4` | SELECT MIN(mi.info) AS release_date, MIN(t.title) AS intern… | 71.4 ms | a hint helped one sample but was slower on another |
| `2a882a48` | SELECT MIN(an.name) AS alternative_name, MIN(chn.name) AS v… | 84.1 ms | a hint helped one sample but was slower on another |
| `ccc29962` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS northern… | 19.6 ms | no other plan was clearly faster |
| `0466dcec` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 24.9 ms | no other plan was clearly faster |
| `bcfea86c` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS movie_ti… | 8.22 ms | no other plan was clearly faster |
| `46c94c7f` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 117 ms | a hint helped one sample but was slower on another |
| `e4943b34` | SELECT MIN(cn.name) AS movie_company, MIN(mi_idx.info) AS r… | 78.7 ms | no other plan was clearly faster |
| `c936eca9` | SELECT MIN(t.title) AS american_movie FROM company_type AS … | 101 ms | a hint helped one sample but was slower on another |
| `43101882` | SELECT MIN(mi.info) AS movie_budget, MIN(mi_idx.info) AS mo… | 154 ms | a hint helped one sample but was slower on another |
| `2ec85d5d` | SELECT MIN(n.name) AS voicing_actress, MIN(t.title) AS kung… | 71.2 ms | a hint helped one sample but was slower on another |
| `5d5082e3` | SELECT MIN(an.name) AS acress_pseudonym, MIN(t.title) AS ja… | 60.8 ms | a hint helped one sample but was slower on another |
| `46b42be8` | SELECT MIN(n.name) AS of_person, MIN(t.title) AS biography_… | 46.9 ms | a hint helped one sample but was slower on another |
| `8f7394ec` | SELECT MIN(t.title) AS typical_european_movie FROM company_… | 98.2 ms | a hint helped one sample but was slower on another |
| `c3f27144` | SELECT MIN(t.title) AS american_vhs_movie FROM company_type… | 99.0 ms | a hint helped one sample but was slower on another |
| `39172b8c` | SELECT MIN(chn.name) AS voiced_char_name, MIN(n.name) AS vo… | 75.1 ms | no other plan was clearly faster |
| `3564c7a4` | SELECT MIN(lt.link) AS link_type, MIN(t1.title) AS first_mo… | 7.23 ms | no other plan was clearly faster |
| `631a5812` | SELECT MIN(chn.name) AS voiced_char, MIN(n.name) AS voicing… | 96.0 ms | no other plan was clearly faster |
| `e14ae77f` | SELECT MIN(mi_idx.info) AS rating, MIN(t.title) AS western_… | 15.4 ms | no other plan was clearly faster |
| `0cf295d8` | SELECT MIN(mc.note) AS production_note, MIN(t.title) AS mov… | 139 ms | a hint helped one sample but was slower on another |
| `ae99dc85` | SELECT MIN(mc.note) AS production_note, MIN(t.title) AS mov… | 138 ms | a hint helped one sample but was slower on another |
| `078dfa3f` | SELECT MIN(mc.note) AS production_note, MIN(t.title) AS mov… | 187 ms | a hint helped one sample but was slower on another |
| `450fe5f6` | SELECT MIN(mc.note) AS production_note, MIN(t.title) AS mov… | 175 ms | a hint helped one sample but was slower on another |
| `c3eba7e4` | SELECT MIN(cn.name) AS from_company, MIN(lt.link) AS movie_… | 10.2 ms | no other plan was clearly faster |
| `3f5cfdf1` | SELECT MIN(kt.kind) AS movie_kind, MIN(t.title) AS complete… | 37.2 ms | no other plan was clearly faster |
| `fbf68f9d` | SELECT MIN(k.keyword) AS movie_keyword, MIN(n.name) AS acto… | 7.42 ms | no other plan was clearly faster |
| `d468dc0f` | SELECT MIN(cn.name) AS from_company, MIN(lt.link) AS movie_… | 10.2 ms | no other plan was clearly faster |
| `ff8164ee` | SELECT MIN(chn.name) AS voiced_char, MIN(n.name) AS voicing… | 91.1 ms | no other plan was clearly faster |
| `05ae6e9d` | SELECT MIN(chn.name) AS voiced_char, MIN(n.name) AS voicing… | 91.4 ms | no other plan was clearly faster |

Times are planning plus execution, as EXPLAIN ANALYZE reports them, median of 3 runs per plan.
