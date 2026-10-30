// Command reference and autocomplete for the console.
// Usages use <name> for a value to fill in and [name] for an optional one;
// the name picks the suggestions (see VALUES). <message...> takes the rest.

const ITEMS = ['diamond', 'netherite_ingot', 'iron_ingot', 'gold_ingot', 'emerald', 'coal', 'redstone', 'lapis_lazuli',
  'oak_log', 'oak_planks', 'cobblestone', 'stone', 'glass', 'torch', 'bread', 'cooked_beef', 'golden_carrot', 'golden_apple',
  'enchanted_golden_apple', 'ender_pearl', 'elytra', 'firework_rocket', 'totem_of_undying', 'experience_bottle', 'shulker_box',
  'diamond_sword', 'diamond_pickaxe', 'diamond_axe', 'diamond_shovel', 'diamond_helmet', 'diamond_chestplate', 'diamond_leggings',
  'diamond_boots', 'netherite_sword', 'netherite_pickaxe', 'netherite_axe', 'netherite_helmet', 'netherite_chestplate',
  'netherite_leggings', 'netherite_boots', 'bow', 'crossbow', 'arrow', 'trident', 'mace', 'shield', 'shears', 'fishing_rod',
  'flint_and_steel', 'bucket', 'water_bucket', 'lava_bucket', 'saddle', 'name_tag', 'lead', 'compass', 'clock', 'map',
  'book', 'writable_book', 'enchanting_table', 'anvil', 'crafting_table', 'furnace', 'chest', 'ender_chest', 'beacon',
  'tnt', 'obsidian', 'bed', 'white_bed', 'spyglass', 'brush', 'trial_key', 'wind_charge', 'command_block', 'barrier', 'light',
  'debug_stick', 'structure_block', 'spawner', 'villager_spawn_egg', 'cow_spawn_egg'];
const EFFECTS = ['speed', 'slowness', 'haste', 'mining_fatigue', 'strength', 'instant_health', 'instant_damage', 'jump_boost',
  'regeneration', 'resistance', 'fire_resistance', 'water_breathing', 'invisibility', 'blindness', 'night_vision', 'hunger',
  'weakness', 'poison', 'wither', 'health_boost', 'absorption', 'saturation', 'glowing', 'levitation', 'luck', 'slow_falling',
  'conduit_power', 'dolphins_grace', 'darkness', 'hero_of_the_village'];
const ENCHANTS = ['sharpness', 'smite', 'bane_of_arthropods', 'knockback', 'fire_aspect', 'looting', 'sweeping_edge',
  'efficiency', 'silk_touch', 'unbreaking', 'fortune', 'power', 'punch', 'flame', 'infinity', 'luck_of_the_sea', 'lure',
  'protection', 'fire_protection', 'blast_protection', 'projectile_protection', 'feather_falling', 'respiration',
  'aqua_affinity', 'thorns', 'depth_strider', 'frost_walker', 'soul_speed', 'swift_sneak', 'mending', 'loyalty', 'impaling',
  'riptide', 'channeling', 'multishot', 'quick_charge', 'piercing', 'density', 'breach', 'wind_burst'];
const ENTITIES = ['zombie', 'skeleton', 'creeper', 'spider', 'enderman', 'witch', 'slime', 'phantom', 'blaze', 'ghast',
  'wither_skeleton', 'piglin', 'pillager', 'evoker', 'ravager', 'breeze', 'warden', 'wither', 'ender_dragon', 'villager',
  'wandering_trader', 'iron_golem', 'cow', 'pig', 'sheep', 'chicken', 'horse', 'donkey', 'wolf', 'cat', 'parrot', 'fox',
  'axolotl', 'allay', 'camel', 'armadillo', 'sniffer', 'armor_stand', 'lightning_bolt', 'tnt', 'item', 'experience_orb'];
const STRUCTURES = ['village_plains', 'village_desert', 'village_savanna', 'village_snowy', 'village_taiga', 'ancient_city',
  'trial_chambers', 'stronghold', 'mansion', 'monument', 'fortress', 'bastion_remnant', 'end_city', 'desert_pyramid',
  'jungle_pyramid', 'igloo', 'pillager_outpost', 'ruined_portal', 'shipwreck', 'buried_treasure', 'mineshaft',
  'trail_ruins', 'swamp_hut', 'ocean_ruin_warm'];
const BIOMES = ['plains', 'forest', 'birch_forest', 'dark_forest', 'taiga', 'jungle', 'desert', 'savanna', 'badlands',
  'swamp', 'mangrove_swamp', 'cherry_grove', 'meadow', 'snowy_plains', 'ice_spikes', 'mushroom_fields', 'ocean',
  'deep_ocean', 'warm_ocean', 'beach', 'river', 'lush_caves', 'deep_dark', 'dripstone_caves', 'pale_garden',
  'jagged_peaks', 'stony_peaks', 'nether_wastes', 'crimson_forest', 'warped_forest', 'basalt_deltas', 'the_end'];

// Game rules were renamed to snake_case in 26.1 (checked in the server jars).
const RULES_NEW = {
  keep_inventory: 'Keep items and XP after dying', advance_time: 'Day and night change', advance_weather: 'Weather changes',
  mob_griefing: 'Creepers, Endermen etc. can change blocks', spawn_mobs: 'Mobs spawn on their own',
  spawn_monsters: 'Monsters spawn', spawn_phantoms: 'Phantoms appear when you skip sleep',
  players_sleeping_percentage: 'Percent of players that must sleep to skip the night', pvp: 'Players can hurt each other',
  show_death_messages: 'Show death messages in chat', show_advancement_messages: 'Announce advancements in chat',
  immediate_respawn: 'Respawn without the death screen', natural_health_regeneration: 'Health regenerates when fed',
  fall_damage: 'Fall damage', fire_damage: 'Fire damage', drowning_damage: 'Drowning damage', freeze_damage: 'Freezing damage',
  tnt_explodes: 'TNT can explode', raids: 'Raids can happen', spawn_patrols: 'Pillager patrols spawn',
  spawn_wandering_traders: 'Wandering traders spawn', spawn_wardens: 'Wardens spawn', block_drops: 'Broken blocks drop items',
  mob_drops: 'Mobs drop loot', entity_drops: 'Minecarts, boats etc. drop as items', random_tick_speed: 'Crop and plant growth speed (default 3)',
  respawn_radius: 'How far from spawn new players appear', locator_bar: 'Show the locator bar',
  send_command_feedback: 'Show command results in chat', command_block_output: 'Command blocks report in chat',
  log_admin_commands: 'Log admin commands', reduced_debug_info: 'Hide coordinates on F3', forgive_dead_players: 'Angry mobs forgive dead players',
  universal_anger: 'Angry neutral mobs attack everyone', spread_vines: 'Vines spread', limited_crafting: 'Only unlocked recipes can be crafted',
  ender_pearls_vanish_on_death: 'Thrown pearls vanish when you die', max_entity_cramming: 'Mobs in one block before they take damage',
  water_source_conversion: 'Water forms new sources', lava_source_conversion: 'Lava forms new sources',
  projectiles_can_break_blocks: 'Projectiles can break blocks', spectators_generate_chunks: 'Spectators load new chunks',
  elytra_movement_check: 'Check elytra speed (anti-cheat)', player_movement_check: 'Check player speed (anti-cheat)',
  fire_spread_radius_around_player: 'How far from players fire spreads', spawner_blocks_work: 'Mob spawners work',
  command_blocks_work: 'Command blocks work', global_sound_events: 'Boss sounds are heard everywhere',
  max_command_sequence_length: 'Command chain limit', max_command_forks: 'Command fork limit', max_block_modifications: 'Block changes per command',
  max_minecart_speed: 'Minecart top speed', max_snow_accumulation_height: 'Snow layers in snowfall',
  block_explosion_drop_decay: 'Block explosions destroy some drops', mob_explosion_drop_decay: 'Mob explosions destroy some drops',
  tnt_explosion_drop_decay: 'TNT destroys some drops', players_nether_portal_default_delay: 'Nether portal delay (survival)',
  players_nether_portal_creative_delay: 'Nether portal delay (creative)', allow_entering_nether_using_portals: 'Nether portals work',
};
const RULES_OLD = {
  keepInventory: 'Keep items and XP after dying', doDaylightCycle: 'Day and night change', doWeatherCycle: 'Weather changes',
  mobGriefing: 'Creepers, Endermen etc. can change blocks', doMobSpawning: 'Mobs spawn on their own',
  doInsomnia: 'Phantoms appear when you skip sleep', playersSleepingPercentage: 'Percent of players that must sleep to skip the night',
  showDeathMessages: 'Show death messages in chat', announceAdvancements: 'Announce advancements in chat',
  doImmediateRespawn: 'Respawn without the death screen', naturalRegeneration: 'Health regenerates when fed',
  fallDamage: 'Fall damage', fireDamage: 'Fire damage', drowningDamage: 'Drowning damage', freezeDamage: 'Freezing damage',
  doFireTick: 'Fire spreads', disableRaids: 'Turn raids off', doPatrolSpawning: 'Pillager patrols spawn',
  doTraderSpawning: 'Wandering traders spawn', doWardenSpawning: 'Wardens spawn', doTileDrops: 'Broken blocks drop items',
  doMobLoot: 'Mobs drop loot', doEntityDrops: 'Minecarts, boats etc. drop as items', randomTickSpeed: 'Crop and plant growth speed (default 3)',
  spawnRadius: 'How far from spawn new players appear', sendCommandFeedback: 'Show command results in chat',
  commandBlockOutput: 'Command blocks report in chat', logAdminCommands: 'Log admin commands', reducedDebugInfo: 'Hide coordinates on F3',
  forgiveDeadPlayers: 'Angry mobs forgive dead players', universalAnger: 'Angry neutral mobs attack everyone',
  doVinesSpread: 'Vines spread', doLimitedCrafting: 'Only unlocked recipes can be crafted', maxEntityCramming: 'Mobs in one block before they take damage',
  tntExplodes: 'TNT can explode', locatorBar: 'Show the locator bar', enderPearlsVanishOnDeath: 'Thrown pearls vanish when you die',
  spectatorsGenerateChunks: 'Spectators load new chunks', waterSourceConversion: 'Water forms new sources',
  lavaSourceConversion: 'Lava forms new sources', projectilesCanBreakBlocks: 'Projectiles can break blocks',
};

function newRuleNames(version) {
  if (!version || version === 'latest') return true;
  const [a, b] = version.split('.').map(n => parseInt(n, 10) || 0);
  return a > 26 || (a === 26 && b >= 1);
}
function gameRules(version) { return newRuleNames(version) ? RULES_NEW : RULES_OLD; }
function ruleName(version, newName, oldName) { return newRuleNames(version) ? newName : oldName; }

// cat: Players, World, Time & weather, Items & effects, Server, Plugins
const COMMANDS = [
  { n: 'list', cat: 'Players', d: 'Show who is online', u: [''] },
  { n: 'say', cat: 'Players', d: 'Send a message to everyone', u: ['<message...>'] },
  { n: 'msg', cat: 'Players', d: 'Send a private message', u: ['<player> <message...>'], a: ['tell', 'w'] },
  { n: 'op', cat: 'Players', d: 'Make a player admin (can use commands)', u: ['<player>'] },
  { n: 'deop', cat: 'Players', d: 'Take admin rights away', u: ['<player>'] },
  { n: 'kick', cat: 'Players', d: 'Throw a player off the server (they can rejoin)', u: ['<player> [reason...]'] },
  { n: 'ban', cat: 'Players', d: 'Ban a player for good', u: ['<player> [reason...]'] },
  { n: 'pardon', cat: 'Players', d: 'Lift a ban', u: ['<player>'] },
  { n: 'ban-ip', cat: 'Players', d: 'Ban an IP address', u: ['<player> [reason...]'] },
  { n: 'pardon-ip', cat: 'Players', d: 'Lift an IP ban', u: ['<ip>'] },
  { n: 'banlist', cat: 'Players', d: 'Show banned players', u: ['', 'players', 'ips'] },
  { n: 'whitelist', cat: 'Players', d: 'Control who may join', u: ['add <player>', 'remove <player>', 'list', 'on', 'off', 'reload'] },
  { n: 'gamemode', cat: 'Players', d: 'Change a game mode', u: ['<gamemode> [player]'] },
  { n: 'tp', cat: 'Players', d: 'Teleport a player to another player or to coordinates', u: ['<player> <target>', '<player> <x> <y> <z>'], a: ['teleport'] },
  { n: 'kill', cat: 'Players', d: 'Kill players or mobs', u: ['[target]'] },
  { n: 'spawnpoint', cat: 'Players', d: 'Set where a player respawns', u: ['[player] [x] [y] [z]'] },
  { n: 'spectate', cat: 'Players', d: 'Watch through another player (spectator mode)', u: ['<target> [player]'] },
  { n: 'title', cat: 'Players', d: 'Show big text on screen', u: ['<player> title <message...>', '<player> subtitle <message...>', '<player> actionbar <message...>', '<player> clear'] },
  { n: 'xp', cat: 'Players', d: 'Give experience', u: ['add <player> <amount> levels', 'add <player> <amount> points', 'set <player> <amount> levels', 'query <player> levels'], a: ['experience'] },
  { n: 'recipe', cat: 'Players', d: 'Unlock all recipes for a player', u: ['give <player> *', 'take <player> *'] },

  { n: 'give', cat: 'Items & effects', d: 'Give an item', u: ['<player> <item> [count]'] },
  { n: 'clear', cat: 'Items & effects', d: 'Empty an inventory', u: ['[player] [item]'] },
  { n: 'effect', cat: 'Items & effects', d: 'Give or remove a potion effect', u: ['give <player> <effect> [seconds] [level]', 'clear <player> [effect]'] },
  { n: 'enchant', cat: 'Items & effects', d: 'Enchant the item a player holds', u: ['<player> <enchantment> [level]'] },

  { n: 'time', cat: 'Time & weather', d: 'Change the time of day', u: ['set <time>', 'add <ticks>', 'query daytime'] },
  { n: 'weather', cat: 'Time & weather', d: 'Change the weather', u: ['clear [seconds]', 'rain [seconds]', 'thunder [seconds]'] },
  { n: 'difficulty', cat: 'Time & weather', d: 'Change the difficulty', u: ['<difficulty>'] },
  { n: 'gamerule', cat: 'World', d: 'Change a game rule, e.g. keep inventory', u: ['<rule> [value]'] },
  { n: 'defaultgamemode', cat: 'World', d: 'Game mode for new players', u: ['<gamemode>'] },
  { n: 'setworldspawn', cat: 'World', d: 'Move the world spawn', u: ['[x] [y] [z]'] },
  { n: 'summon', cat: 'World', d: 'Spawn a mob or entity', u: ['<entity> [x] [y] [z]'] },
  { n: 'locate', cat: 'World', d: 'Find the nearest structure or biome', u: ['structure <structure>', 'biome <biome>'] },
  { n: 'seed', cat: 'World', d: 'Show the world seed', u: [''] },
  { n: 'worldborder', cat: 'World', d: 'Set the size of the world', u: ['set <size>', 'center <x> <z>', 'get'] },
  { n: 'tick', cat: 'World', d: 'Slow down, speed up or freeze the game', u: ['query', 'rate <tps>', 'freeze', 'unfreeze', 'sprint <ticks>'] },
  { n: 'forceload', cat: 'World', d: 'Keep chunks loaded', u: ['add <x> <z>', 'remove <x> <z>', 'query'] },

  { n: 'save-all', cat: 'Server', d: 'Save the world now', u: ['', 'flush'] },
  { n: 'save-off', cat: 'Server', d: 'Pause automatic saving', u: [''] },
  { n: 'save-on', cat: 'Server', d: 'Resume automatic saving', u: [''] },
  { n: 'stop', cat: 'Server', d: 'Save and stop the server', u: [''] },
  { n: 'reload', cat: 'Server', d: 'Reload data packs', u: [''] },
  { n: 'datapack', cat: 'Server', d: 'Manage data packs', u: ['list', 'enable <pack>', 'disable <pack>'] },
  { n: 'help', cat: 'Server', d: 'List every command the server knows', u: ['[command]'] },

  { n: 'plugins', cat: 'Plugins', d: 'List installed plugins', u: [''], a: ['pl'], only: 'paper' },
  { n: 'version', cat: 'Plugins', d: 'Show the server version', u: ['[plugin]'], only: 'paper' },
  { n: 'tps', cat: 'Plugins', d: 'Server speed (20 is perfect)', u: [''], only: 'paper' },
  { n: 'mspt', cat: 'Plugins', d: 'Time per tick (under 50 ms is good)', u: [''], only: 'paper' },
  { n: 'chunky', cat: 'Plugins', d: 'Pre-generate the world so it lags less later', u: ['radius <size>', 'start', 'pause', 'continue', 'cancel', 'progress'], mod: 'chunky' },
  { n: 'spark', cat: 'Plugins', d: 'Find what causes lag', u: ['tps', 'health', 'profiler start', 'profiler stop'], mod: 'spark' },
];

const VALUES = {
  player: ctx => [...ctx.players, '@a', '@p', '@r', '@s'],
  target: ctx => [...ctx.players, '@a', '@p', '@r', '@e', '@e[type=item]'],
  gamemode: () => ['survival', 'creative', 'adventure', 'spectator'],
  difficulty: () => ['peaceful', 'easy', 'normal', 'hard'],
  item: () => ITEMS, effect: () => EFFECTS, enchantment: () => ENCHANTS, entity: () => ENTITIES,
  structure: () => STRUCTURES, biome: () => BIOMES,
  time: () => ['day', 'noon', 'night', 'midnight'],
  rule: ctx => Object.keys(gameRules(ctx.version)),
  value: (ctx, prev) => {
    const r = prev[prev.length - 1];
    return /speed|percentage|radius|max_|height|delay|cramming|length|limit|count|Speed|Percentage|Radius|max[A-Z]|Height|Delay|Cramming|Length/.test(r || '') ? [] : ['true', 'false'];
  },
  x: () => ['~'], y: () => ['~'], z: () => ['~'],
  level: () => ['1', '2', '3', '5', '10'], count: () => ['1', '16', '32', '64'],
  amount: () => ['1', '5', '10', '30', '100'], seconds: () => ['30', '60', '300', '600'],
};

const HINTS = {
  player: 'a player name, or @a = everyone, @p = nearest', target: 'a player, or @e = all entities',
  x: 'coordinate, ~ = where you are', y: 'height, ~ = where you are', z: 'coordinate, ~ = where you are',
  ticks: '20 ticks = 1 second, 24000 = one day', time: 'day, night or a number of ticks',
  seconds: 'how long', level: 'strength, 1 is normal', size: 'width in blocks',
  value: 'true / false, or a number', count: 'how many (max 64 per stack)',
};

function availableCommands(ctx) {
  return COMMANDS.filter(c => (!c.only || c.only === ctx.serverType) && (!c.mod || ctx.mods.includes(c.mod)));
}
function findCommand(ctx, name) {
  name = name.replace(/^\//, '').toLowerCase();
  return availableCommands(ctx).find(c => c.n === name || (c.a || []).includes(name));
}
const isArg = p => /^[<[]/.test(p);
const argName = p => p.replace(/^[<[]|[\]>]$/g, '').replace(/\.\.\.$/, '');

// suggest returns { items: [{value, label, desc}], hint: {cmd, usage, index, arg} }.
function suggest(input, ctx) {
  const text = input.replace(/^\//, '');
  const parts = text.split(' ');
  const current = parts[parts.length - 1].toLowerCase();
  const done = parts.slice(0, -1);
  if (done.length === 0) {
    const items = availableCommands(ctx)
      .filter(c => c.n.startsWith(current) || (c.a || []).some(a => a.startsWith(current)))
      .map(c => ({ value: c.n + ' ', label: c.n, desc: c.d, usage: c.u.filter(Boolean)[0] || '' }));
    return { items: items.slice(0, 12), hint: null };
  }
  const cmd = findCommand(ctx, done[0]);
  if (!cmd) return { items: [], hint: null };
  const typed = done.slice(1);
  let hint = null;
  const seen = new Set();
  const items = [];
  for (const usage of cmd.u) {
    const pat = usage ? usage.split(' ') : [];
    let ok = true, rest = false;
    for (let i = 0; i < typed.length; i++) {
      const p = pat[i];
      if (p === undefined) { ok = false; break; }
      if (p.endsWith('...]') || p.endsWith('...>')) { rest = true; break; }
      if (!isArg(p) && p.toLowerCase() !== typed[i].toLowerCase()) { ok = false; break; }
    }
    if (!ok) continue;
    const idx = rest ? pat.findIndex(p => p.endsWith('...>') || p.endsWith('...]')) : typed.length;
    const p = pat[idx];
    if (!hint && p !== undefined) hint = { cmd: cmd.n, usage: pat, index: idx, desc: cmd.d, arg: isArg(p) ? argName(p) : null };
    if (p === undefined || rest) continue;
    const opts = isArg(p) ? (VALUES[argName(p)] ? VALUES[argName(p)](ctx, typed) : []) : [p];
    for (const o of opts) {
      if (o.toLowerCase().startsWith(current) && !seen.has(o)) {
        seen.add(o);
        const rule = argName(p) === 'rule' ? gameRules(ctx.version)[o] : '';
        items.push({ value: [done.join(' '), o].join(' ') + ' ', label: o, desc: rule || (isArg(p) ? '' : 'option') });
      }
    }
  }
  if (!hint) hint = { cmd: cmd.n, usage: (cmd.u.find(Boolean) || '').split(' ').filter(Boolean), index: -1, desc: cmd.d, arg: null };
  return { items: items.slice(0, 40), hint };
}

window.MCCommands = { COMMANDS, HINTS, suggest, availableCommands, gameRules, ruleName, newRuleNames };
