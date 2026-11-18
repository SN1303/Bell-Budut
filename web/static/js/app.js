// Application State
let isAdmin = false;
let adminToken = localStorage.getItem('bell_admin_token') || '';
let webAudioEnabled = false;
let currentTab = 'schedules';
let globalGroups = [];
let globalAudios = [];
let audioPlayer = document.getElementById('browserAudioPlayer');
let sseConnection = null;

// ==================== INITIALIZATION ====================

window.addEventListener('DOMContentLoaded', async () => {
  const pillToday = document.getElementById('pillTodayName');
  if (pillToday) pillToday.innerText = `(${currentDayNameIndo})`;
  initSSE();
  initWebAudio();
  checkAuthStatus();
  await loadGroups();
  loadAudios();
  loadSchedules();
  loadSettings();
  loadLogs();
  loadWindowsSoundsStatus();
});

// ==================== SSE REAL-TIME LISTENER ====================

function initSSE() {
  if (sseConnection) sseConnection.close();

  sseConnection = new EventSource('/api/events');

  sseConnection.onmessage = function(event) {
    try {
      const data = JSON.parse(event.data);

      if (data.type === 'TICK') {
        // 1. Live Clock & Date
        document.getElementById('liveClock').innerText = data.server_time;
        document.getElementById('liveDate').innerText = data.server_date;

        if (data.day_name) {
          currentDayNameIndo = data.day_name;
          const pillToday = document.getElementById('pillTodayName');
          if (pillToday) {
            pillToday.innerText = `(${data.day_name})`;
          }
        }

        // 2. Active Group
        if (data.active_group) {
          document.getElementById('activeGroupName').innerText = data.active_group.name;
          const badge = document.getElementById('groupBadge');
          if (data.active_group.determined_by === 'DATE_RANGE') {
            badge.innerText = `Otomatis (${data.active_group.start_date} s/d ${data.active_group.end_date})`;
            badge.className = 'px-2 py-0.5 rounded text-xs font-semibold bg-green-100 text-green-800';
            document.getElementById('activeGroupInfo').innerText = 'Grup ini aktif otomatis berdasarkan rentang tanggal';
          } else {
            badge.innerText = 'Pilihan Manual';
            badge.className = 'px-2 py-0.5 rounded text-xs font-semibold bg-blue-100 text-blue-800';
            document.getElementById('activeGroupInfo').innerText = 'Grup aktif yang dipilih secara manual oleh operator';
          }
        }

        // 3. Holiday Mode
        const holidayBadge = document.getElementById('badgeHoliday');
        const holidayBanner = document.getElementById('holidayBanner');
        if (data.holiday_mode) {
          holidayBadge.classList.remove('hidden');
          holidayBanner.classList.remove('hidden');
        } else {
          holidayBadge.classList.add('hidden');
          holidayBanner.classList.add('hidden');
        }

        // 4. Next Bell
        if (data.next_bell) {
          document.getElementById('nextBellCountdown').innerText = data.next_bell.countdown_display;
          document.getElementById('nextBellTimeLabel').innerText = `(Pukul ${data.next_bell.time})`;
          document.getElementById('nextBellTitle').innerText = data.next_bell.title;
          document.getElementById('nextBellAudioCode').innerText = `Kode Audio: ${data.next_bell.audio_code} - ${data.next_bell.audio_name || ''}`;
          document.getElementById('nextBellNotes').innerText = data.next_bell.notes || 'Sesuai jadwal reguler';
        } else {
          document.getElementById('nextBellCountdown').innerText = 'SELESAI';
          document.getElementById('nextBellTimeLabel').innerText = '';
          document.getElementById('nextBellTitle').innerText = 'Tidak ada bel lagi hari ini';
          document.getElementById('nextBellAudioCode').innerText = 'Kode Audio: -';
          document.getElementById('nextBellNotes').innerText = 'Semua jam bel untuk hari ini sudah berlalu';
        }

        // 5. Server Speaker Status
        const speakerStatus = document.getElementById('serverSpeakerStatus');
        if (data.is_server_playing) {
          speakerStatus.innerText = `Speaker: 🔊 Memutar (${data.current_playing})`;
          speakerStatus.className = 'text-xs text-green-600 font-bold animate-pulse';

          // Sync activePlayback with current_code from server if available!
          if (data.current_code) {
            const isDifferent = !activePlayback || 
              activePlayback.targetType !== 'server' || 
              (activePlayback.code || '').toLowerCase() !== data.current_code.toLowerCase();
            
            if (isDifferent) {
              const audioObj = (globalAudios || []).find(a => (a.code || '').toLowerCase() === data.current_code.toLowerCase());
              activePlayback = {
                targetType: 'server',
                targetId: `srv-${data.current_code}`,
                code: data.current_code,
                name: audioObj ? audioObj.name : (data.current_playing || data.current_code),
                schedId: null
              };
              updateAllPlayButtonsUI();
            }
          }

          if (!activePlayback || (activePlayback.targetType !== 'sched' && activePlayback.targetType !== 'web')) {
            showFloatingPlayerBar('Speaker Server (Soundcard)', data.current_playing);
          }
        } else {
          speakerStatus.innerText = data.server_audio_enabled ? 'Speaker Server: Siap' : 'Speaker Server: Nonaktif';
          speakerStatus.className = 'text-xs text-gray-500';

          // Grace period: Do not let stale TICK cancel active server playback within 2.5s of click!
          const elapsed = Date.now() - (lastServerPlayTriggerTime || 0);
          if (activePlayback && activePlayback.targetType === 'server' && elapsed > 2500) {
            activePlayback = null;
            hideFloatingPlayerBar();
            updateAllPlayButtonsUI();
          }
        }

      } else if (data.type === 'SERVER_AUDIO_STOPPED') {
        const elapsed = Date.now() - (lastServerPlayTriggerTime || 0);
        if (activePlayback && activePlayback.targetType === 'server') {
          // If specific audio_code was stopped, only clear if it matches OR grace period elapsed
          if (!data.audio_code || (activePlayback.code || '').toLowerCase() === (data.audio_code || '').toLowerCase() || elapsed > 2500) {
            activePlayback = null;
            hideFloatingPlayerBar();
            updateAllPlayButtonsUI();
          }
        }
      } else if (data.type === 'SERVER_AUDIO_STARTED') {
        const audioObj = (globalAudios || []).find(a => (a.code || '').toLowerCase() === (data.audio_code || '').toLowerCase());
        activePlayback = {
          targetType: 'server',
          targetId: `srv-${data.audio_code}`,
          code: data.audio_code,
          name: audioObj ? audioObj.name : (data.audio_name || data.audio_code),
          schedId: null
        };
        lastServerPlayTriggerTime = Date.now();
        updateAllPlayButtonsUI();
        showFloatingPlayerBar('Speaker Server (Soundcard)', `[${data.audio_code}] ${data.audio_name}`);
      } else if (data.type === 'BELL_RING') {
        // Live Bell Ring Event
        handleBellRingEvent(data);
      }
    } catch (e) {
      console.error('SSE JSON error:', e);
    }
  };

  sseConnection.onerror = function() {
    console.warn('SSE Disconnected, reconnecting in 3s...');
  };
}

function handleBellRingEvent(data) {
  // 1. Show Ring Banner
  const banner = document.getElementById('liveBellBanner');
  document.getElementById('liveBellTitle').innerText = `🔔 ${data.title} (${data.time})`;
  document.getElementById('liveBellSubtitle').innerText = `Kode: ${data.audio_code} | Nada: ${data.audio_name} | Grup: ${data.group_name}`;
  banner.classList.remove('hidden');
  banner.classList.add('bell-ring-active');

  setTimeout(() => {
    banner.classList.add('hidden');
    banner.classList.remove('bell-ring-active');
  }, 10000);

  // 2. Play Web Audio (if enabled by operator)
  if (webAudioEnabled && data.audio_url) {
    audioPlayer.src = data.audio_url;
    audioPlayer.play().catch(e => console.log('Web audio autoplay was prevented:', e));
  }

  showToast(`Bel Berbunyi: ${data.title}`, 'info');
  loadLogs();
}

// ==================== WEB AUDIO HYBRID TOGGLE ====================

function initWebAudio() {
  const saved = localStorage.getItem('bell_web_audio_enabled');
  webAudioEnabled = saved === 'true';
  updateWebAudioUI();
}

function toggleWebAudio() {
  webAudioEnabled = !webAudioEnabled;
  localStorage.setItem('bell_web_audio_enabled', webAudioEnabled ? 'true' : 'false');
  updateWebAudioUI();

  if (webAudioEnabled) {
    // Unlock AudioContext by playing a silent sound
    audioPlayer.play().then(() => audioPlayer.pause()).catch(() => {});
    showToast('Suara Bel di browser ini DIAKTIFKAN', 'success');
  } else {
    audioPlayer.pause();
    showToast('Suara Bel di browser ini DINONAKTIFKAN', 'info');
  }
}

function updateWebAudioUI() {
  const icon = document.getElementById('webAudioIcon');
  const text = document.getElementById('webAudioText');
  const btn = document.getElementById('btnWebAudioToggle');

  if (webAudioEnabled) {
    icon.innerText = '🔊';
    text.innerText = 'Suara Browser: Aktif';
    btn.className = 'flex items-center space-x-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold bg-green-700 hover:bg-green-600 transition';
  } else {
    icon.innerText = '🔇';
    text.innerText = 'Suara Browser: Nonaktif';
    btn.className = 'flex items-center space-x-1.5 px-3 py-1.5 rounded-lg text-xs font-semibold bg-blue-800 hover:bg-blue-700 transition';
  }
}

// ==================== AUTHENTICATION & PIN ====================

async function checkAuthStatus() {
  if (adminToken) {
    try {
      const res = await fetch('/api/auth/status', {
        headers: { 'X-Admin-Token': adminToken }
      });
      const json = await res.json();
      if (json.success && json.data.is_admin) {
        isAdmin = true;
      } else {
        isAdmin = false;
        adminToken = '';
        localStorage.removeItem('bell_admin_token');
      }
    } catch (e) {
      isAdmin = true;
    }
  } else {
    isAdmin = false;
  }
  updateAuthUI();
}

function handleAuthClick() {
  if (isAdmin) {
    // Logout
    fetch('/api/logout', { method: 'POST' }).finally(() => {
      isAdmin = false;
      adminToken = '';
      localStorage.removeItem('bell_admin_token');
      updateAuthUI();
      showToast('Berhasil keluar dari mode Admin', 'info');
    });
  } else {
    openModal('modalPIN');
    document.getElementById('modalInputPIN').value = '';
    document.getElementById('pinErrorMessage').classList.add('hidden');
    document.getElementById('modalInputPIN').focus();
  }
}

async function submitVerifyPIN() {
  const pin = document.getElementById('modalInputPIN').value.trim();
  if (!pin) return;

  try {
    const res = await fetch('/api/verify-pin', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ pin })
    });
    const json = await res.json();

    if (json.success) {
      isAdmin = true;
      adminToken = json.data.token;
      localStorage.setItem('bell_admin_token', adminToken);
      closeModal('modalPIN');
      updateAuthUI();
      showToast('Masuk Admin berhasil', 'success');
    } else {
      document.getElementById('pinErrorMessage').classList.remove('hidden');
    }
  } catch (err) {
    showToast('Gagal menghubungi server', 'error');
  }
}

function updateAuthUI() {
  const btn = document.getElementById('btnAuth');
  const text = document.getElementById('btnAuthText');
  if (isAdmin) {
    text.innerText = 'Keluar Admin';
    btn.className = 'flex items-center space-x-1 px-3 py-1.5 rounded-lg text-xs font-bold bg-green-600 hover:bg-green-500 text-white transition';
  } else {
    text.innerText = 'Masuk Admin';
    btn.className = 'flex items-center space-x-1 px-3 py-1.5 rounded-lg text-xs font-bold bg-yellow-500 hover:bg-yellow-400 text-blue-950 transition';
  }
}

function requireAdmin(actionName) {
  if (!isAdmin) {
    showToast(`Perlu PIN Admin untuk ${actionName}`, 'warning');
    handleAuthClick();
    return false;
  }
  return true;
}

// Fetch wrapper with auth header
async function authFetch(url, options = {}) {
  options.headers = options.headers || {};
  if (adminToken) {
    options.headers['X-Admin-Token'] = adminToken;
  }
  const res = await fetch(url, options);
  if (res.status === 401) {
    isAdmin = false;
    adminToken = '';
    localStorage.removeItem('bell_admin_token');
    updateAuthUI();
    showToast('Sesi Admin berakhir. Silakan masukkan PIN kembali.', 'warning');
    handleAuthClick();
    throw new Error('Unauthorized');
  }
  return res;
}

// ==================== TAB NAVIGATION ====================

function switchTab(tabId) {
  currentTab = tabId;

  // Toggle button styles
  document.querySelectorAll('.tab-btn').forEach(btn => {
    btn.className = 'tab-btn py-3 px-1 border-b-2 border-transparent text-gray-500 hover:text-gray-700 whitespace-nowrap';
  });
  const activeBtn = document.getElementById(`tabBtn-${tabId}`);
  if (activeBtn) {
    activeBtn.className = 'tab-btn py-3 px-1 border-b-2 border-blue-900 text-blue-900 whitespace-nowrap font-bold';
  }

  // Toggle content
  document.querySelectorAll('.tab-content').forEach(c => c.classList.add('hidden'));
  const activeContent = document.getElementById(`tabContent-${tabId}`);
  if (activeContent) {
    activeContent.classList.remove('hidden');
  }

  // Reload data for this tab
  if (tabId === 'schedules') loadSchedules();
  if (tabId === 'groups') loadGroups();
  if (tabId === 'audio') loadAudios();
  if (tabId === 'backup') loadLogs();
}

// ==================== GROUPS MANAGEMENT ====================

async function loadGroups() {
  try {
    const res = await fetch('/api/groups');
    const json = await res.json();
    if (!json.success) return;

    globalGroups = json.data || [];

    // Find active group
    const activeG = globalGroups.find(g => g.is_active) || globalGroups[0];

    // Populate Group Filter in Schedules Tab
    const select = document.getElementById('scheduleFilterGroup');
    const formGroupSelect = document.getElementById('formScheduleGroup');
    const previousSelection = select ? select.value : null;

    if (select) {
      select.innerHTML = '';
    }
    if (formGroupSelect) {
      formGroupSelect.innerHTML = '';
    }

    globalGroups.forEach(g => {
      if (select) {
        const opt = document.createElement('option');
        opt.value = g.id;
        if (g.is_active) {
          opt.innerText = `⭐ [AKTIF HARI INI] ${g.name}`;
          opt.className = 'font-bold text-green-800 bg-green-50';
        } else {
          opt.innerText = g.name;
        }
        select.appendChild(opt);
      }

      if (formGroupSelect) {
        const opt2 = document.createElement('option');
        opt2.value = g.id;
        opt2.innerText = g.name;
        formGroupSelect.appendChild(opt2);
      }
    });

    if (select) {
      // Option for all groups at the bottom
      const optAll = document.createElement('option');
      optAll.value = '0';
      optAll.innerText = '-- Tampilkan Semua Grup Sekaligus --';
      select.appendChild(optAll);

      // Auto-select active group by default
      if (previousSelection && previousSelection !== '0' && globalGroups.some(x => x.id == previousSelection)) {
        select.value = previousSelection;
      } else if (activeG) {
        select.value = activeG.id;
      }
    }

    // Populate Excel Template Target Group Select
    const tplSelect = document.getElementById('templateTargetGroupSelect');
    if (tplSelect) {
      const prevTplVal = tplSelect.value;
      tplSelect.innerHTML = '<option value="">(Umum / Bel Reguler)</option>';
      globalGroups.forEach(g => {
        const opt = document.createElement('option');
        opt.value = g.id;
        opt.innerText = g.is_active ? `⭐ ${g.name} (Aktif)` : g.name;
        tplSelect.appendChild(opt);
      });
      if (prevTplVal) tplSelect.value = prevTplVal;
    }

    // Populate Excel Export Group Select
    const expSelect = document.getElementById('exportGroupSelect');
    if (expSelect) {
      const prevExpVal = expSelect.value;
      expSelect.innerHTML = '<option value="all">Semua Grup Sekaligus (Gabungan)</option>';
      globalGroups.forEach(g => {
        const opt = document.createElement('option');
        opt.value = g.id;
        opt.innerText = g.is_active ? `⭐ ${g.name} (Aktif)` : g.name;
        expSelect.appendChild(opt);
      });
      if (prevExpVal) expSelect.value = prevExpVal;
    }

    // Populate Excel Import Target Group Select
    const impSelect = document.getElementById('importTargetGroupSelect');
    if (impSelect) {
      const prevImpVal = impSelect.value;
      impSelect.innerHTML = '';
      globalGroups.forEach(g => {
        const opt = document.createElement('option');
        opt.value = g.id;
        opt.innerText = g.is_active ? `⭐ ${g.name} (Aktif Saat Ini)` : g.name;
        impSelect.appendChild(opt);
      });
      if (prevImpVal) {
        impSelect.value = prevImpVal;
      } else if (activeG) {
        impSelect.value = activeG.id;
      }
    }

    // Populate Groups Cards in Tab 2
    const container = document.getElementById('groupsList');
    container.innerHTML = '';

    globalGroups.forEach(g => {
      const card = document.createElement('div');
      card.className = `p-5 rounded-2xl border transition bg-white shadow-sm flex flex-col justify-between ${
        g.is_active ? 'border-blue-600 ring-2 ring-blue-100' : 'border-gray-200'
      }`;

      let rangeInfo = '<span class="text-gray-400">Rentang tanggal tidak aktif</span>';
      if (g.date_range_enabled && g.start_date && g.end_date) {
        rangeInfo = `<span class="text-green-700 font-semibold">📅 ${g.start_date} s/d ${g.end_date}</span>`;
      }

      card.innerHTML = `
        <div>
          <div class="flex items-center justify-between">
            <span class="text-xs font-bold uppercase tracking-wider ${g.is_active ? 'text-blue-600' : 'text-gray-400'}">
              ${g.is_default ? 'Grup Utama (Default)' : 'Grup Tambahan'}
            </span>
            ${g.is_active ? '<span class="px-2 py-0.5 rounded text-xs font-bold bg-blue-100 text-blue-800">Sedang Aktif</span>' : ''}
          </div>
          <h4 class="text-lg font-bold text-gray-800 mt-1">${g.name}</h4>
          <p class="text-xs text-gray-500 mt-1">${g.description || 'Tidak ada keterangan'}</p>
          <div class="text-xs bg-gray-50 p-2.5 rounded-lg border border-gray-100 mt-3 space-y-1">
            <div class="flex justify-between"><span>Jumlah Jadwal:</span> <b class="font-mono text-gray-800">${g.schedule_count} Bel</b></div>
            <div class="flex justify-between"><span>Otomatisasi:</span> ${rangeInfo}</div>
          </div>
        </div>

        <div class="mt-4 pt-3 border-t border-gray-100 space-y-2">
          <div class="flex gap-2">
            ${!g.is_active ? `
              <button onclick="activateGroup(${g.id})" class="flex-1 py-1.5 bg-blue-900 hover:bg-blue-800 text-white rounded-lg text-xs font-semibold transition">
                Aktifkan Sekarang
              </button>
            ` : `
              <span class="flex-1 py-1.5 text-center text-xs text-blue-700 font-bold bg-blue-50 rounded-lg">
                ✓ Grup Aktif
              </span>
            `}
            <button onclick="openDateRangeModal(${g.id})" class="px-3 py-1.5 bg-gray-100 hover:bg-gray-200 text-gray-700 rounded-lg text-xs font-semibold transition" title="Setel Rentang Tanggal">
              📅 Rentang
            </button>
          </div>
          <div class="flex justify-end space-x-3 text-xs pt-1">
            <button onclick="editGroup(${g.id})" class="text-blue-700 hover:underline">Edit Nama</button>
            ${!g.is_default ? `<button onclick="deleteGroup(${g.id})" class="text-red-600 hover:underline">Hapus</button>` : ''}
          </div>
        </div>
      `;
      container.appendChild(card);
    });

  } catch (err) {
    console.error('Failed to load groups:', err);
  }
}

async function activateGroup(id) {
  if (!requireAdmin('mengaktifkan grup')) return;
  try {
    const res = await authFetch(`/api/groups/activate/${id}`, { method: 'POST' });
    const json = await res.json();
    if (json.success) {
      showToast(json.data, 'success');
      await loadGroups();
      const sel = document.getElementById('scheduleFilterGroup');
      if (sel) sel.value = id;
      loadSchedules();
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

function openGroupModal(id = 0) {
  if (!requireAdmin('mengelola grup')) return;
  document.getElementById('formGroupID').value = id || 0;
  if (id) {
    const g = globalGroups.find(x => x.id === id);
    if (g) {
      document.getElementById('groupModalTitle').innerText = 'Edit Grup Jadwal';
      document.getElementById('formGroupName').value = g.name;
      document.getElementById('formGroupDesc').value = g.description;
    }
  } else {
    document.getElementById('groupModalTitle').innerText = 'Tambah Grup Jadwal Baru';
    document.getElementById('formGroupName').value = '';
    document.getElementById('formGroupDesc').value = '';
  }
  openModal('modalGroup');
}

function editGroup(id) {
  openGroupModal(id);
}

async function saveGroup() {
  const id = parseInt(document.getElementById('formGroupID').value);
  const name = document.getElementById('formGroupName').value.trim();
  const description = document.getElementById('formGroupDesc').value.trim();

  if (!name) {
    showToast('Nama grup tidak boleh kosong', 'warning');
    return;
  }

  const url = id ? `/api/groups/update/${id}` : '/api/groups/create';
  try {
    const res = await authFetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, description })
    });
    const json = await res.json();
    if (json.success) {
      showToast('Grup berhasil disimpan', 'success');
      closeModal('modalGroup');
      loadGroups();
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

async function deleteGroup(id) {
  if (!requireAdmin('menghapus grup')) return;
  if (!confirm('Yakin ingin menghapus grup ini beserta semua jadwal di dalamnya?')) return;

  try {
    const res = await authFetch(`/api/groups/delete/${id}`, { method: 'DELETE' });
    const json = await res.json();
    if (json.success) {
      showToast(json.data, 'success');
      loadGroups();
      loadSchedules();
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

function openDateRangeModal(id) {
  if (!requireAdmin('mengatur rentang tanggal')) return;
  const g = globalGroups.find(x => x.id === id);
  if (!g) return;

  document.getElementById('rangeGroupID').value = id;
  document.getElementById('rangeEnabled').checked = g.date_range_enabled;
  document.getElementById('rangeStartDate').value = g.start_date || '';
  document.getElementById('rangeEndDate').value = g.end_date || '';
  openModal('modalDateRange');
}

async function saveDateRange() {
  const id = document.getElementById('rangeGroupID').value;
  const enabled = document.getElementById('rangeEnabled').checked;
  const start_date = document.getElementById('rangeStartDate').value;
  const end_date = document.getElementById('rangeEndDate').value;

  try {
    const res = await authFetch(`/api/groups/date-range/${id}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled, start_date, end_date })
    });
    const json = await res.json();
    if (json.success) {
      showToast('Rentang tanggal jadwal berhasil disimpan', 'success');
      closeModal('modalDateRange');
      loadGroups();
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

// ==================== SCHEDULES MANAGEMENT ====================

const indonesianDays = ['MINGGU', 'SENIN', 'SELASA', 'RABU', 'KAMIS', 'JUMAT', 'SABTU'];
let currentDayNameIndo = indonesianDays[new Date().getDay()] || 'SENIN';
let selectedDayFilter = 'TODAY'; // Default to TODAY for clean single-day timeline
let loadedScheduleData = [];
let currentTimeSort = 'asc';

function setDayFilter(day) {
  selectedDayFilter = day;

  // Sync with column dropdown filter
  const colFilterDay = document.getElementById('colFilterDay');
  if (colFilterDay) {
    colFilterDay.value = day;
  }

  // Update day pill buttons
  document.querySelectorAll('.day-pill').forEach(btn => {
    btn.className = 'day-pill px-3 py-2 rounded-xl font-bold bg-gray-100 hover:bg-gray-200 text-gray-700 transition';
  });

  const activePill = document.getElementById(`dayPill-${day}`);
  if (activePill) {
    activePill.className = 'day-pill px-3.5 py-2 rounded-xl font-extrabold bg-blue-900 text-white shadow transition flex items-center space-x-1.5';
  }

  loadSchedules();
}

function onColFilterDayChange(day) {
  setDayFilter(day);
}

function toggleTimeSort() {
  currentTimeSort = (currentTimeSort === 'asc') ? 'desc' : 'asc';
  const icon = document.getElementById('sortTimeIcon');
  if (icon) {
    icon.innerText = (currentTimeSort === 'asc') ? '▲' : '▼';
  }
  applyColumnFilters();
}

async function loadSchedules() {
  const groupID = document.getElementById('scheduleFilterGroup').value || 0;
  let day = selectedDayFilter;
  if (day === 'TODAY') {
    day = currentDayNameIndo;
  }

  try {
    const res = await fetch(`/api/schedules?group_id=${groupID}&day=${day}`);
    const json = await res.json();
    if (!json.success) return;

    loadedScheduleData = json.data || [];

    // Populate colFilterAudio with audios present in loaded data
    populateAudioFilterDropdown(loadedScheduleData);

    // Apply all column filters (Waktu, Title, Audio, Status, Sort)
    applyColumnFilters();

  } catch (err) {
    console.error('Failed to load schedules:', err);
  }
}

function populateAudioFilterDropdown(items) {
  const select = document.getElementById('colFilterAudio');
  if (!select) return;
  const currentVal = select.value;
  select.innerHTML = '<option value="ALL">Semua Nada Audio</option>';

  const audioMap = new Map();
  items.forEach(s => {
    if (s.audio_code && !audioMap.has(s.audio_code)) {
      audioMap.set(s.audio_code, s.audio_name || s.audio_code);
    }
  });

  Array.from(audioMap.entries()).sort((a, b) => a[0].localeCompare(b[0])).forEach(([code, name]) => {
    const opt = document.createElement('option');
    opt.value = code;
    opt.innerText = `${code}: ${name}`;
    select.appendChild(opt);
  });

  if (currentVal && audioMap.has(currentVal)) {
    select.value = currentVal;
  }
}

function applyColumnFilters() {
  const colTime = (document.getElementById('colFilterTime')?.value) || 'ALL';
  const colTitle = (document.getElementById('colFilterTitle')?.value || '').toLowerCase().trim();
  const searchInput = (document.getElementById('scheduleSearchInput')?.value || '').toLowerCase().trim();
  const titleQuery = colTitle || searchInput;
  const colAudio = (document.getElementById('colFilterAudio')?.value) || 'ALL';
  const colStatus = (document.getElementById('colFilterStatus')?.value) || 'ALL';

  let filtered = loadedScheduleData.filter(s => {
    // 1. Time range filter
    if (colTime === 'PAGI' && s.time >= '10:00') return false;
    if (colTime === 'SIANG' && (s.time < '10:00' || s.time >= '13:00')) return false;
    if (colTime === 'SORE' && s.time < '13:00') return false;

    // 2. Title & notes query
    if (titleQuery) {
      const combined = `${s.time} ${s.day_of_week} ${s.title} ${s.audio_code} ${s.audio_name || ''} ${s.notes || ''}`.toLowerCase();
      if (!combined.includes(titleQuery)) return false;
    }

    // 3. Audio code filter
    if (colAudio !== 'ALL') {
      if (s.audio_code.toLowerCase() !== colAudio.toLowerCase()) return false;
    }

    // 4. Status filter
    if (colStatus !== 'ALL') {
      const isEn = colStatus === '1';
      if (s.is_enabled !== isEn) return false;
    }

    return true;
  });

  // Sort by time
  filtered.sort((a, b) => {
    if (currentTimeSort === 'asc') {
      return a.time.localeCompare(b.time) || (a.id - b.id);
    } else {
      return b.time.localeCompare(a.time) || (b.id - a.id);
    }
  });

  renderScheduleRows(filtered);
  updateScheduleStatsLabel(filtered.length, loadedScheduleData.length);
}

function updateScheduleStatsLabel(count, total) {
  const statsEl = document.getElementById('scheduleTableStats');
  if (!statsEl) return;
  const dayLabel = selectedDayFilter === 'TODAY' 
    ? `Hari Ini (${currentDayNameIndo})` 
    : (selectedDayFilter === 'ALL' ? 'Ringkasan Master (Semua)' : `Hari ${selectedDayFilter}`);

  if (count !== undefined && total !== undefined && count !== total) {
    statsEl.innerText = `Menampilkan ${count} dari ${total} jadwal (${dayLabel})`;
  } else {
    const n = count !== undefined ? count : loadedScheduleData.length;
    statsEl.innerText = `Menampilkan ${n} jadwal (${dayLabel})`;
  }
}

function resetAllScheduleFilters() {
  const colFilterTime = document.getElementById('colFilterTime');
  if (colFilterTime) colFilterTime.value = 'ALL';

  const colFilterTitle = document.getElementById('colFilterTitle');
  if (colFilterTitle) colFilterTitle.value = '';

  const scheduleSearchInput = document.getElementById('scheduleSearchInput');
  if (scheduleSearchInput) scheduleSearchInput.value = '';

  const colFilterAudio = document.getElementById('colFilterAudio');
  if (colFilterAudio) colFilterAudio.value = 'ALL';

  const colFilterStatus = document.getElementById('colFilterStatus');
  if (colFilterStatus) colFilterStatus.value = 'ALL';

  currentTimeSort = 'asc';
  const icon = document.getElementById('sortTimeIcon');
  if (icon) icon.innerText = '▲';

  applyColumnFilters();
  showToast('Semua filter kolom telah dibersihkan', 'info');
}

function filterScheduleTable() {
  applyColumnFilters();
}

function renderScheduleRows(items) {
  const tbody = document.getElementById('scheduleTableBody');
  tbody.innerHTML = '';

  if (items.length === 0) {
    tbody.innerHTML = `<tr><td colspan="6" class="py-12 text-center text-gray-400 text-sm">Tidak ada jadwal yang cocok dengan filter.</td></tr>`;
    return;
  }

  items.forEach(s => {
    const tr = document.createElement('tr');
    tr.className = 'hover:bg-blue-50/60 transition border-b border-gray-100 schedule-row';

    tr.innerHTML = `
      <td class="py-3 px-3 font-mono font-extrabold text-base text-blue-950 whitespace-nowrap">
        <span class="inline-block py-0.5 px-2 rounded-lg bg-gray-100 border border-gray-200 shadow-2xs">${s.time}</span>
      </td>
      <td class="py-3 px-3">
        ${renderVisualDayBubbles(s.day_of_week)}
      </td>
      <td class="py-3 px-3">
        <div class="font-bold text-gray-900 text-sm leading-tight">${s.title}</div>
        ${s.notes ? `<div class="text-[11px] text-gray-400 mt-0.5 leading-tight">${s.notes}</div>` : ''}
      </td>
      <td class="py-3 px-3">
        <div class="flex items-center space-x-1.5">
          <button onclick="handlePlayScheduleRow(${s.id}, '${s.audio_code}')" id="btn-play-sched-${s.id}" data-sched-id="${s.id}" data-code="${s.audio_code}" title="Dengarkan di web browser" class="btn-preview-sched w-7 h-7 rounded-full bg-blue-100 hover:bg-blue-200 text-blue-900 flex items-center justify-center text-xs font-bold transition shadow-2xs flex-shrink-0 cursor-pointer">
            ▶
          </button>
          <span class="font-mono text-xs font-bold ${s.audio_code.startsWith('FLD') ? 'bg-amber-100 text-amber-900 border-amber-300' : 'bg-gray-100 text-gray-800 border-gray-200'} px-1.5 py-0.5 rounded border flex-shrink-0">${s.audio_code}</span>
          <span class="text-xs text-gray-700 font-semibold truncate max-w-[170px]" title="${s.audio_name || ''}">${s.audio_name || ''}</span>
        </div>
      </td>
      <td class="py-3 px-3 text-center">
        <button onclick="toggleScheduleState(${s.id})" class="px-2.5 py-1 rounded-full text-xs font-bold transition shadow-2xs ${
          s.is_enabled ? 'bg-green-100 text-green-800 hover:bg-green-200 border border-green-200' : 'bg-gray-200 text-gray-600 hover:bg-gray-300'
        }">
          ${s.is_enabled ? '✓ Aktif' : 'Nonaktif'}
        </button>
      </td>
      <td class="py-3 px-3 text-right space-x-1 whitespace-nowrap">
        <button onclick="editSchedule(${JSON.stringify(s).replace(/"/g, '&quot;')})" class="text-blue-700 hover:text-blue-900 font-bold text-xs bg-blue-50 hover:bg-blue-100 px-2.5 py-1 rounded-lg transition">Edit</button>
        <button onclick="deleteSchedule(${s.id})" class="text-red-600 hover:text-red-800 font-bold text-xs bg-red-50 hover:bg-red-100 px-2 py-1 rounded-lg transition">Hapus</button>
      </td>
    `;
    tbody.appendChild(tr);
  });
  updatePlayButtonsState();
}

// Clean Visual Day Bubbles (No more mumet reading long strings!)
function renderVisualDayBubbles(dayStr) {
  // If specific day filter is active (not ALL), show clean simple badge with recurring pattern
  if (selectedDayFilter !== 'ALL') {
    const dayBadge = (selectedDayFilter === 'TODAY') ? currentDayNameIndo : selectedDayFilter;
    let subtitle = '';
    const d = dayStr.toUpperCase();
    if (d === 'SENIN - JUMAT' || d === 'SEMUA') {
      subtitle = '<span class="text-[10px] text-gray-500 font-medium">Rutin (Senin - Jumat)</span>';
    } else if (d === 'SELASA - JUMAT') {
      subtitle = '<span class="text-[10px] text-indigo-700 font-semibold bg-indigo-50 px-1 py-0.5 rounded">Rutin (Selasa - Jumat)</span>';
    } else if (d === 'SENIN - KAMIS') {
      subtitle = '<span class="text-[10px] text-sky-700 font-semibold bg-sky-50 px-1 py-0.5 rounded">Rutin (Senin - Kamis)</span>';
    } else if (d === 'SENIN - RABU') {
      subtitle = '<span class="text-[10px] text-blue-700 font-semibold bg-blue-50 px-1 py-0.5 rounded">Rutin (Senin - Rabu)</span>';
    } else if (d === 'SENIN') {
      subtitle = '<span class="text-[10px] text-purple-800 font-bold bg-purple-100 px-1.5 py-0.5 rounded border border-purple-200">⭐ Khusus Upacara</span>';
    } else if (d === 'KAMIS') {
      subtitle = '<span class="text-[10px] text-amber-800 font-bold bg-amber-100 px-1.5 py-0.5 rounded border border-amber-200">⭐ Khusus Pulang 14:15</span>';
    } else if (d === 'JUMAT') {
      subtitle = '<span class="text-[10px] text-teal-800 font-bold bg-teal-100 px-1.5 py-0.5 rounded border border-teal-200">⭐ Khusus Shalat Jumat</span>';
    } else {
      subtitle = `<span class="text-[10px] text-gray-500">${dayStr}</span>`;
    }

    return `
      <div class="flex flex-col space-y-0.5">
        <div class="flex items-center space-x-1.5">
          <span class="inline-flex items-center px-2 py-0.5 rounded-md text-xs font-black bg-blue-100 text-blue-900 border border-blue-200 shadow-2xs">
            ✓ ${dayBadge}
          </span>
        </div>
        <div>${subtitle}</div>
      </div>
    `;
  }

  // In ALL view, show Alarm-style active day circles: [S] [S] [R] [K] [J]
  const weekDays = [
    { key: 'SENIN', letter: 'S', title: 'Senin' },
    { key: 'SELASA', letter: 'S', title: 'Selasa' },
    { key: 'RABU', letter: 'R', title: 'Rabu' },
    { key: 'KAMIS', letter: 'K', title: 'Kamis' },
    { key: 'JUMAT', letter: 'J', title: 'Jumat' }
  ];

  const d = dayStr.toUpperCase();
  const activeKeys = new Set();

  if (d === 'SEMUA' || d === 'SENIN - JUMAT') {
    weekDays.forEach(w => activeKeys.add(w.key));
  } else if (d === 'SENIN - KAMIS') {
    ['SENIN', 'SELASA', 'RABU', 'KAMIS'].forEach(k => activeKeys.add(k));
  } else if (d === 'SELASA - JUMAT') {
    ['SELASA', 'RABU', 'KAMIS', 'JUMAT'].forEach(k => activeKeys.add(k));
  } else if (d === 'SENIN - RABU') {
    ['SENIN', 'SELASA', 'RABU'].forEach(k => activeKeys.add(k));
  } else {
    weekDays.forEach(w => {
      if (d.includes(w.key)) activeKeys.add(w.key);
    });
  }

  let html = `<div class="space-y-1" title="Berlaku: ${dayStr}">`;
  html += `<div class="flex items-center space-x-1">`;
  weekDays.forEach(w => {
    const isAct = activeKeys.has(w.key);
    if (isAct) {
      html += `<span class="w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-extrabold bg-blue-900 text-white shadow-2xs" title="${w.title}">${w.letter}</span>`;
    } else {
      html += `<span class="w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-bold bg-gray-100 text-gray-300" title="Tidak aktif">${w.letter}</span>`;
    }
  });
  html += `</div>`;

  // Short human text badge
  let labelText = dayStr;
  let labelColor = 'text-gray-700 bg-gray-100';
  if (d === 'SENIN - JUMAT' || d === 'SEMUA') {
    labelText = 'Senin – Jumat (Setiap Hari)';
    labelColor = 'text-green-800 bg-green-50 border border-green-200';
  } else if (d === 'SENIN - KAMIS') {
    labelText = 'Senin – Kamis';
    labelColor = 'text-sky-800 bg-sky-50 border border-sky-200';
  } else if (d === 'SELASA - JUMAT') {
    labelText = 'Selasa – Jumat';
    labelColor = 'text-indigo-800 bg-indigo-50 border border-indigo-200';
  } else if (d === 'SENIN - RABU') {
    labelText = 'Senin – Rabu';
    labelColor = 'text-blue-800 bg-blue-50 border border-blue-200';
  } else if (d === 'JUMAT') {
    labelText = '⭐ Khusus Jumat (Shalat)';
    labelColor = 'text-teal-800 bg-teal-50 border border-teal-200';
  } else if (d === 'KAMIS') {
    labelText = '⭐ Khusus Kamis (Pulang 14:15)';
    labelColor = 'text-amber-800 bg-amber-50 border border-amber-200';
  } else if (d === 'SENIN') {
    labelText = '⭐ Khusus Senin (Upacara)';
    labelColor = 'text-purple-800 bg-purple-50 border border-purple-200';
  }

  html += `<div><span class="text-[10px] font-extrabold px-1.5 py-0.5 rounded ${labelColor}">${labelText}</span></div></div>`;
  return html;
}

// Matriks Mingguan Modal Logic
async function openWeeklyMatrixModal() {
  const groupID = document.getElementById('scheduleFilterGroup').value || 0;
  const matrixBody = document.getElementById('weeklyMatrixBody');
  matrixBody.innerHTML = '<tr><td colspan="5" class="py-8 text-center text-gray-500">Memuat matriks mingguan...</td></tr>';
  openModal('modalWeeklyMatrix');

  try {
    const res = await fetch(`/api/schedules?group_id=${groupID}&day=ALL`);
    const json = await res.json();
    if (!json.success || !json.data) {
      matrixBody.innerHTML = '<tr><td colspan="5" class="py-8 text-center text-red-500">Gagal memuat jadwal.</td></tr>';
      return;
    }

    const allSchedules = json.data;
    if (allSchedules.length === 0) {
      matrixBody.innerHTML = '<tr><td colspan="5" class="py-8 text-center text-gray-400">Tidak ada jadwal untuk grup ini.</td></tr>';
      return;
    }

    const times = Array.from(new Set(allSchedules.map(s => s.time))).sort();

    function appliesToDay(dayOfWeek, targetDay) {
      const d = dayOfWeek.toUpperCase();
      if (d === 'SEMUA' || d === 'SENIN - JUMAT') return true;
      if (targetDay === 'SENIN') {
        return d === 'SENIN' || d === 'SENIN - KAMIS' || d === 'SENIN - RABU' || d.includes('SENIN');
      }
      if (targetDay === 'RABU') {
        return d === 'RABU' || d === 'SELASA' || d === 'SENIN - KAMIS' || d === 'SELASA - JUMAT' || d === 'SENIN - RABU' || d.includes('RABU') || d.includes('SELASA');
      }
      if (targetDay === 'KAMIS') {
        return d === 'KAMIS' || d === 'SENIN - KAMIS' || d === 'SELASA - JUMAT' || d.includes('KAMIS');
      }
      if (targetDay === 'JUMAT') {
        return d === 'JUMAT' || d === 'SELASA - JUMAT' || d.includes('JUMAT');
      }
      return false;
    }

    matrixBody.innerHTML = '';
    times.forEach(t => {
      const schedSenin = allSchedules.filter(s => s.time === t && appliesToDay(s.day_of_week, 'SENIN'));
      const schedSelRab = allSchedules.filter(s => s.time === t && appliesToDay(s.day_of_week, 'RABU'));
      const schedKamis = allSchedules.filter(s => s.time === t && appliesToDay(s.day_of_week, 'KAMIS'));
      const schedJumat = allSchedules.filter(s => s.time === t && appliesToDay(s.day_of_week, 'JUMAT'));

      function renderCell(items) {
        if (!items || items.length === 0) {
          return '<span class="text-gray-300 italic">-</span>';
        }
        return items.map(it => {
          let badge = '';
          const d = it.day_of_week.toUpperCase();
          if (d === 'SENIN') badge = '<span class="px-1.5 py-0.2 rounded text-[10px] bg-purple-100 text-purple-800 font-bold ml-1">Upacara</span>';
          else if (d === 'KAMIS') badge = '<span class="px-1.5 py-0.2 rounded text-[10px] bg-amber-100 text-amber-800 font-bold ml-1">Pulang</span>';
          else if (d === 'JUMAT') badge = '<span class="px-1.5 py-0.2 rounded text-[10px] bg-teal-100 text-teal-800 font-bold ml-1">Jumat</span>';

          return `<div class="font-semibold text-gray-800 text-xs py-0.5">
            ${it.title} ${badge}
            <div class="text-[10px] text-gray-400 font-mono">${it.audio_code}</div>
          </div>`;
        }).join('');
      }

      const tr = document.createElement('tr');
      tr.className = 'hover:bg-blue-50/40 transition border-b border-gray-100';
      tr.innerHTML = `
        <td class="py-2 px-3 font-mono font-black text-xs text-blue-950 bg-gray-50/70 border-r border-gray-200">${t}</td>
        <td class="py-2 px-3 border-r border-gray-100">${renderCell(schedSenin)}</td>
        <td class="py-2 px-3 border-r border-gray-100">${renderCell(schedSelRab)}</td>
        <td class="py-2 px-3 border-r border-gray-100 bg-amber-50/20">${renderCell(schedKamis)}</td>
        <td class="py-2 px-3 bg-teal-50/20">${renderCell(schedJumat)}</td>
      `;
      matrixBody.appendChild(tr);
    });

  } catch (err) {
    matrixBody.innerHTML = '<tr><td colspan="5" class="py-8 text-center text-red-500">Terjadi kesalahan memuat matriks mingguan.</td></tr>';
  }
}

function openScheduleModal() {
  if (!requireAdmin('menambah jadwal')) return;
  document.getElementById('scheduleModalTitle').innerText = 'Tambah Jadwal Bel';
  document.getElementById('formScheduleID').value = '';
  document.getElementById('formScheduleTime').value = '07:00';
  document.getElementById('formScheduleName').value = '';
  document.getElementById('formScheduleNotes').value = '';
  openModal('modalSchedule');
}

function editSchedule(s) {
  if (!requireAdmin('mengedit jadwal')) return;
  document.getElementById('scheduleModalTitle').innerText = 'Edit Jadwal Bel';
  document.getElementById('formScheduleID').value = s.id;
  document.getElementById('formScheduleGroup').value = s.group_id;
  document.getElementById('formScheduleDay').value = s.day_of_week;
  document.getElementById('formScheduleTime').value = s.time;
  document.getElementById('formScheduleName').value = s.title;
  document.getElementById('formScheduleAudio').value = s.audio_code;
  document.getElementById('formScheduleNotes').value = s.notes || '';
  openModal('modalSchedule');
}

async function saveSchedule() {
  const id = document.getElementById('formScheduleID').value;
  const group_id = parseInt(document.getElementById('formScheduleGroup').value);
  const day_of_week = document.getElementById('formScheduleDay').value;
  const time = document.getElementById('formScheduleTime').value;
  const title = document.getElementById('formScheduleName').value.trim();
  const audio_code = document.getElementById('formScheduleAudio').value;
  const notes = document.getElementById('formScheduleNotes').value.trim();

  if (!title || !time || !audio_code) {
    showToast('Waktu, Nama Bel, dan Nada Audio wajib diisi', 'warning');
    return;
  }

  const payload = {
    group_id,
    day_of_week,
    time,
    title,
    audio_code,
    notes,
    is_enabled: true
  };

  const url = id ? `/api/schedules/update/${id}` : '/api/schedules/create';
  try {
    const res = await authFetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const json = await res.json();
    if (json.success) {
      showToast('Jadwal berhasil disimpan', 'success');
      closeModal('modalSchedule');
      loadSchedules();
      loadGroups();
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

async function toggleScheduleState(id) {
  if (!requireAdmin('mengubah status jadwal')) return;
  try {
    const res = await authFetch(`/api/schedules/toggle/${id}`, { method: 'POST' });
    const json = await res.json();
    if (json.success) {
      loadSchedules();
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

async function deleteSchedule(id) {
  if (!requireAdmin('menghapus jadwal')) return;
  if (!confirm('Hapus jadwal bel ini?')) return;

  try {
    const res = await authFetch(`/api/schedules/delete/${id}`, { method: 'DELETE' });
    const json = await res.json();
    if (json.success) {
      showToast('Jadwal dihapus', 'success');
      loadSchedules();
      loadGroups();
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

// ==================== AUDIO MANAGEMENT ====================

function escapeHtml(str) {
  if (!str) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

async function loadAudios() {
  try {
    const res = await fetch('/api/audio');
    const json = await res.json();
    if (!json.success) return;

    globalAudios = json.data || [];

    // Populate Schedule Form Dropdown
    const select = document.getElementById('formScheduleAudio');
    select.innerHTML = '';
    globalAudios.forEach(a => {
      const opt = document.createElement('option');
      opt.value = a.code;
      const prefix = a.is_folder ? '📁 [FOLDER RANDOM] ' : '🎵 ';
      opt.innerText = `${prefix}${a.code} - ${a.name}`;
      select.appendChild(opt);
    });

    // Populate Audio Table in Tab 3
    const tbody = document.getElementById('audioTableBody');
    tbody.innerHTML = '';

    globalAudios.forEach(a => {
      const tr = document.createElement('tr');
      tr.className = a.is_folder ? 'bg-amber-50 hover:bg-amber-100 transition' : 'hover:bg-gray-50 transition';
      tr.innerHTML = `
        <td class="py-3 px-4 font-mono font-bold ${a.is_folder ? 'text-amber-800' : 'text-green-800'}">
          ${a.code}
          ${a.is_folder ? '<span class="ml-1.5 px-2 py-0.5 rounded text-xs bg-amber-200 text-amber-900 font-bold">FOLDER ACAK</span>' : ''}
        </td>
        <td class="py-3 px-4 font-semibold text-gray-800">
          <div class="flex items-center gap-2 flex-wrap">
            <span>${a.is_folder ? '📁 ' : ''}${escapeHtml(a.name)}</span>
            ${a.is_folder ? `
              <button onclick="openFolderManageModal('${a.code}', '${escapeHtml(a.name)}')" class="inline-flex items-center gap-1 px-2.5 py-1 rounded-lg text-xs font-bold bg-amber-200 hover:bg-amber-300 text-amber-950 border border-amber-400 shadow-xs transition cursor-pointer">
                <span>📂</span><span>Kelola Isi Lagu</span>
              </button>
            ` : ''}
          </div>
        </td>
        <td class="py-3 px-4 text-xs font-mono text-gray-500">${a.filename}</td>
        <td class="py-3 px-4 text-xs text-gray-500">${a.description || '-'}</td>
        <td class="py-3 px-4 text-center">
          <button onclick="handlePlayAudioWeb('${a.code}')" id="btn-play-web-${a.code}" data-code="${a.code}" data-folder="${a.is_folder}" class="btn-browser-audio px-2.5 py-1.5 ${a.is_folder ? 'bg-amber-200 hover:bg-amber-300 text-amber-900' : 'bg-blue-100 hover:bg-blue-200 text-blue-800'} rounded-lg text-xs font-semibold transition cursor-pointer">
            ▶ ${a.is_folder ? 'Dengar Acak di Web' : 'Dengar di Web'}
          </button>
        </td>
        <td class="py-3 px-4 text-center">
          <button onclick="handlePlayAudioServer('${a.code}')" id="btn-play-srv-${a.code}" data-code="${a.code}" data-folder="${a.is_folder}" class="btn-server-audio px-2.5 py-1.5 ${a.is_folder ? 'bg-amber-600 hover:bg-amber-700 text-white' : 'bg-green-100 hover:bg-green-200 text-green-800'} rounded-lg text-xs font-semibold transition cursor-pointer">
            🔊 ${a.is_folder ? 'Bunyikan Acak di Server' : 'Bunyikan di Speaker'}
          </button>
        </td>
        <td class="py-3 px-4 text-right">
          ${!a.is_builtin ? `
            <button onclick="deleteAudio(${a.id})" class="text-red-600 hover:underline font-semibold text-xs">Hapus</button>
          ` : `
            <span class="text-gray-400 text-xs italic">Bawaan</span>
          `}
        </td>
      `;
      tbody.appendChild(tr);
    });
    updatePlayButtonsState();

  } catch (err) {
    console.error('Failed to load audios:', err);
  }
}

// ==================== GLOBAL EXCLUSIVE AUDIO CONTROLLER ====================
// Guarantees:
// 1. Strictly ONE audio plays at any time (mutual exclusion across web and server soundcard).
// 2. Clicking any new song immediately stops the previous song completely.
// 3. The Stop button (⏹) appears ONLY on the currently playing (latest clicked) song.
// 4. Clicking the Stop button on the active song stops it and returns the button to ▶.
// 5. When the song finishes naturally, the button returns to ▶.

let activePlayback = null;
let lastServerPlayTriggerTime = 0;
// Structure of activePlayback:
// {
//   targetType: 'sched' | 'web' | 'server',
//   targetId: 'sched-12' | 'web-AUD-01' | 'srv-AUD-01',
//   code: 'AUD-01',
//   name: 'Nama Lagu',
//   schedId: 12
// }

function formatAudioTime(seconds) {
  if (isNaN(seconds) || seconds < 0) return '00:00';
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

function showFloatingPlayerBar(modeText, titleText) {
  const bar = document.getElementById('floatingAudioPlayerBar');
  const modeEl = document.getElementById('playerBarMode');
  const titleEl = document.getElementById('playerBarTitle');
  const navStop = document.getElementById('navbarStopBtn');
  if (modeEl) modeEl.innerText = modeText;
  if (titleEl) titleEl.innerText = titleText;
  if (bar) bar.classList.remove('hidden');
  if (navStop) navStop.classList.remove('hidden');
}

function hideFloatingPlayerBar() {
  const bar = document.getElementById('floatingAudioPlayerBar');
  const navStop = document.getElementById('navbarStopBtn');
  if (bar) bar.classList.add('hidden');
  if (navStop) navStop.classList.add('hidden');
  const liveBanner = document.getElementById('liveBellBanner');
  if (liveBanner) liveBanner.classList.add('hidden');
}

// Resets ALL play buttons across the UI and sets the STOP button ONLY on activePlayback
function updateAllPlayButtonsUI() {
  // 1. Schedule Table Buttons (Tab 1)
  document.querySelectorAll('.btn-preview-sched').forEach(btn => {
    const schedId = parseInt(btn.dataset.schedId, 10);
    const isThisPlaying = activePlayback && activePlayback.targetType === 'sched' && activePlayback.schedId === schedId;

    if (isThisPlaying) {
      btn.innerHTML = '⏹';
      btn.className = 'btn-preview-sched w-7 h-7 rounded-full bg-red-600 hover:bg-red-700 text-white flex items-center justify-center text-xs font-bold animate-pulse shadow-md flex-shrink-0 cursor-pointer transition';
      btn.title = 'Hentikan pemutaran audio ini';
    } else {
      btn.innerHTML = '▶';
      btn.className = 'btn-preview-sched w-7 h-7 rounded-full bg-blue-100 hover:bg-blue-200 text-blue-900 flex items-center justify-center text-xs font-bold transition shadow-2xs flex-shrink-0 cursor-pointer';
      btn.title = 'Dengarkan di web browser';
    }
  });

  // 2. Audio Library Web Buttons (Tab 3)
  document.querySelectorAll('.btn-browser-audio').forEach(btn => {
    const code = btn.dataset.code;
    const isFld = btn.dataset.folder === 'true';
    const isThisPlaying = activePlayback && activePlayback.targetType === 'web' && activePlayback.code.toLowerCase() === (code || '').toLowerCase();

    if (isThisPlaying) {
      btn.innerHTML = '⏹ Hentikan Web';
      btn.className = 'btn-browser-audio px-2.5 py-1.5 bg-red-600 hover:bg-red-700 text-white rounded-lg text-xs font-bold shadow animate-pulse transition cursor-pointer';
      btn.title = 'Hentikan pemutaran lagu ini';
    } else {
      btn.innerHTML = isFld ? '▶ Dengar Acak di Web' : '▶ Dengar di Web';
      btn.className = `btn-browser-audio px-2.5 py-1.5 ${isFld ? 'bg-amber-200 hover:bg-amber-300 text-amber-900' : 'bg-blue-100 hover:bg-blue-200 text-blue-800'} rounded-lg text-xs font-semibold transition cursor-pointer`;
      btn.title = 'Dengarkan pratinjau lagu ini di browser';
    }
  });

  // 3. Audio Library Server Buttons (Tab 3)
  document.querySelectorAll('.btn-server-audio').forEach(btn => {
    const code = btn.dataset.code;
    const isFld = btn.dataset.folder === 'true';
    const isThisPlaying = activePlayback && activePlayback.targetType === 'server' && activePlayback.code.toLowerCase() === (code || '').toLowerCase();

    if (isThisPlaying) {
      btn.innerHTML = '⏹ Hentikan Server';
      btn.className = 'btn-server-audio px-2.5 py-1.5 bg-red-600 hover:bg-red-700 text-white rounded-lg text-xs font-bold shadow animate-pulse transition cursor-pointer';
      btn.title = 'Hentikan pemutaran lagu di speaker server';
    } else {
      btn.innerHTML = isFld ? '🔊 Acak di Server' : '🔊 Bunyikan di Speaker';
      btn.className = `btn-server-audio px-2.5 py-1.5 ${isFld ? 'bg-amber-600 hover:bg-amber-700 text-white' : 'bg-green-100 hover:bg-green-200 text-green-800'} rounded-lg text-xs font-semibold transition cursor-pointer`;
      btn.title = 'Bunyikan lagu ini di speaker server';
    }
  });
}

// Stop all audio silently (synchronous reset of HTML5 element + async fire to server MCI)
function stopBrowserAudio() {
  if (!audioPlayer) {
    audioPlayer = document.getElementById('browserAudioPlayer');
  }
  if (audioPlayer) {
    try {
      audioPlayer.pause();
      audioPlayer.currentTime = 0;
      audioPlayer.removeAttribute('src');
      audioPlayer.load();
    } catch (e) {}
  }
}

async function stopServerAudio() {
  activePlayback = null;
  hideFloatingPlayerBar();
  updateAllPlayButtonsUI();
  try {
    await fetch('/api/audio/stop-server', { method: 'POST' });
  } catch (err) {}
}

function stopAllAudioSilent() {
  stopBrowserAudio();
  if (activePlayback && activePlayback.targetType === 'server') {
    fetch('/api/audio/stop-server', { method: 'POST' }).catch(() => {});
  }
  activePlayback = null;
  hideFloatingPlayerBar();
  updateAllPlayButtonsUI();
}

function stopAllAudio() {
  stopBrowserAudio();
  if (activePlayback && activePlayback.targetType === 'server') {
    fetch('/api/audio/stop-server', { method: 'POST' }).catch(() => {});
  }
  activePlayback = null;
  hideFloatingPlayerBar();
  updateAllPlayButtonsUI();
  showToast('Semua pemutaran audio dihentikan', 'info');
}

// 1. Play button in Schedule Table
async function handlePlayScheduleRow(schedId, audioCode) {
  // If the user clicked the button that is already playing -> STOP IT!
  if (activePlayback && activePlayback.targetType === 'sched' && activePlayback.schedId === schedId) {
    stopBrowserAudio();
    activePlayback = null;
    hideFloatingPlayerBar();
    updateAllPlayButtonsUI();
    showToast('Pratinjau jadwal dihentikan', 'info');
    return;
  }

  // If server sound was playing, stop it cleanly
  if (activePlayback && activePlayback.targetType === 'server') {
    fetch('/api/audio/stop-server', { method: 'POST' }).catch(() => {});
  }
  stopBrowserAudio();

  const audio = globalAudios.find(a => a.code.toLowerCase() === audioCode.toLowerCase());
  const audioName = audio ? audio.name : audioCode;

  // Register new active playback
  activePlayback = {
    targetType: 'sched',
    targetId: `sched-${schedId}`,
    code: audioCode,
    name: audioName,
    schedId: schedId
  };

  updateAllPlayButtonsUI();
  showFloatingPlayerBar('Pratinjau Jadwal Web', `[${audioCode}] ${audioName}`);

  if (!audioPlayer) audioPlayer = document.getElementById('browserAudioPlayer');

  audioPlayer.onended = () => {
    if (activePlayback && activePlayback.targetType === 'sched' && activePlayback.schedId === schedId) {
      activePlayback = null;
      hideFloatingPlayerBar();
      updateAllPlayButtonsUI();
    }
  };
  audioPlayer.ontimeupdate = () => {
    const prog = document.getElementById('playerBarProgress');
    if (prog) {
      prog.innerText = `${formatAudioTime(audioPlayer.currentTime)} / ${formatAudioTime(audioPlayer.duration || 0)}`;
    }
  };

  try {
    audioPlayer.src = `/api/audio/stream/${encodeURIComponent(audioCode)}?t=${Date.now()}`;
    await audioPlayer.play();
  } catch (err) {
    console.warn('Playback interrupted or blocked:', err);
  }
}

// 2. Play "Dengar di Web" in Audio Library
async function handlePlayAudioWeb(code) {
  // If user clicked the button that is already playing on web -> STOP IT!
  if (activePlayback && activePlayback.targetType === 'web' && activePlayback.code.toLowerCase() === code.toLowerCase()) {
    stopBrowserAudio();
    activePlayback = null;
    hideFloatingPlayerBar();
    updateAllPlayButtonsUI();
    showToast('Pratinjau web dihentikan', 'info');
    return;
  }

  // If server sound was playing, stop it cleanly
  if (activePlayback && activePlayback.targetType === 'server') {
    fetch('/api/audio/stop-server', { method: 'POST' }).catch(() => {});
  }
  stopBrowserAudio();

  const audio = globalAudios.find(a => a.code.toLowerCase() === code.toLowerCase());
  const audioName = audio ? audio.name : code;

  activePlayback = {
    targetType: 'web',
    targetId: `web-${code}`,
    code: code,
    name: audioName,
    schedId: null
  };

  updateAllPlayButtonsUI();
  showFloatingPlayerBar('Pratinjau Web Browser', `[${code}] ${audioName}`);

  if (!audioPlayer) audioPlayer = document.getElementById('browserAudioPlayer');

  audioPlayer.onended = () => {
    if (activePlayback && activePlayback.targetType === 'web' && activePlayback.code.toLowerCase() === code.toLowerCase()) {
      activePlayback = null;
      hideFloatingPlayerBar();
      updateAllPlayButtonsUI();
    }
  };
  audioPlayer.ontimeupdate = () => {
    const prog = document.getElementById('playerBarProgress');
    if (prog) {
      prog.innerText = `${formatAudioTime(audioPlayer.currentTime)} / ${formatAudioTime(audioPlayer.duration || 0)}`;
    }
  };

  try {
    audioPlayer.src = `/api/audio/stream/${encodeURIComponent(code)}?t=${Date.now()}`;
    await audioPlayer.play();
  } catch (err) {
    console.warn('Playback interrupted or blocked:', err);
  }
}

// 3. Play "Bunyikan di Speaker" in Audio Library
async function handlePlayAudioServer(code) {
  // If user clicked the button that is ALREADY playing on server -> STOP IT!
  if (activePlayback && activePlayback.targetType === 'server' && activePlayback.code.toLowerCase() === code.toLowerCase()) {
    await stopServerAudio();
    showToast('Pemutaran speaker server dihentikan', 'info');
    return;
  }

  // 1. Instantly silence any browser audio
  stopBrowserAudio();

  const audio = globalAudios.find(a => a.code.toLowerCase() === code.toLowerCase());
  const audioName = audio ? audio.name : code;

  lastServerPlayTriggerTime = Date.now();

  // 2. Register new active playback immediately (reverts previous song button to 🔊, sets this one to ⏹)
  activePlayback = {
    targetType: 'server',
    targetId: `srv-${code}`,
    code: code,
    name: audioName,
    schedId: null
  };

  updateAllPlayButtonsUI();
  showFloatingPlayerBar('Speaker Server (Soundcard)', `[${code}] ${audioName}`);

  // 3. Send test-server command (Go server handles track-switch atomically without client race conditions)
  try {
    const res = await fetch('/api/audio/test-server', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code })
    });
    const json = await res.json();
    if (json.success) {
      showToast(json.data || `Memutar [${code}] di speaker server`, 'info');
    } else {
      showToast(json.error || 'Gagal memutar audio di server', 'error');
      if (activePlayback && activePlayback.targetType === 'server' && activePlayback.code.toLowerCase() === code.toLowerCase()) {
        activePlayback = null;
        hideFloatingPlayerBar();
        updateAllPlayButtonsUI();
      }
    }
  } catch (err) {
    showToast('Gagal terhubung ke server', 'error');
    if (activePlayback && activePlayback.targetType === 'server' && activePlayback.code.toLowerCase() === code.toLowerCase()) {
      activePlayback = null;
      hideFloatingPlayerBar();
      updateAllPlayButtonsUI();
    }
  }
}

// Legacy aliases for backward compatibility
function previewAudioBrowser(code) {
  handlePlayAudioWeb(code);
}
function testPlayServer(code) {
  handlePlayAudioServer(code);
}
function updatePlayButtonsState() {
  updateAllPlayButtonsUI();
}
function stopServerAudio() {
  stopAllAudio();
}

let selectedAudioFileList = [];
let audioDropzoneInitialized = false;

function openAudioUploadModal() {
  if (!requireAdmin('mengunggah audio')) return;
  initAudioDropzone();
  clearSelectedAudioFiles();
  openModal('modalAudioUpload');
}

function initAudioDropzone() {
  if (audioDropzoneInitialized) return;
  const dropArea = document.getElementById('audioDropArea');
  if (!dropArea) return;
  ['dragenter', 'dragover'].forEach(name => {
    dropArea.addEventListener(name, (e) => {
      e.preventDefault();
      e.stopPropagation();
      dropArea.classList.add('border-green-600', 'bg-green-100');
    }, false);
  });
  ['dragleave', 'drop'].forEach(name => {
    dropArea.addEventListener(name, (e) => {
      e.preventDefault();
      e.stopPropagation();
      dropArea.classList.remove('border-green-600', 'bg-green-100');
    }, false);
  });
  dropArea.addEventListener('drop', (e) => {
    const dt = e.dataTransfer;
    const files = dt.files;
    if (files && files.length > 0) {
      handleAudioFilesSelected(files);
    }
  }, false);
  audioDropzoneInitialized = true;
}

function clearSelectedAudioFiles() {
  const input = document.getElementById('uploadAudioFiles');
  if (input) input.value = '';
  selectedAudioFileList = [];
  const chk = document.getElementById('chkAsFolder');
  if (chk) chk.checked = false;
  const fldName = document.getElementById('uploadFolderName');
  if (fldName) fldName.value = '';
  renderSelectedAudioFiles();
}

function handleAudioFilesSelected(files) {
  selectedAudioFileList = Array.from(files || []).filter(f => {
    const ext = f.name.toLowerCase();
    return ext.endsWith('.mp3') || ext.endsWith('.wav') || ext.endsWith('.ogg');
  });
  renderSelectedAudioFiles();
}

function populateTargetFolderSelect() {
  const sel = document.getElementById('selectTargetFolderCode');
  if (!sel) return;
  sel.innerHTML = '';
  const folders = (globalAudios || []).filter(a => a.is_folder);
  if (folders.length === 0) {
    const opt = document.createElement('option');
    opt.value = '';
    opt.innerText = '(Belum ada folder playlist, buat baru)';
    sel.appendChild(opt);
    return;
  }
  folders.forEach(f => {
    const opt = document.createElement('option');
    opt.value = f.code;
    opt.innerText = `[${f.code}] ${f.name}`;
    sel.appendChild(opt);
  });
}

function toggleFolderModeUI() {
  const isExisting = document.getElementById('radFolderModeExisting')?.checked;
  const divExisting = document.getElementById('divSelectExistingFolder');
  const divNew = document.getElementById('divInputNewFolder');
  if (isExisting) {
    if (divExisting) divExisting.classList.remove('hidden');
    if (divNew) divNew.classList.add('hidden');
  } else {
    if (divExisting) divExisting.classList.add('hidden');
    if (divNew) divNew.classList.remove('hidden');
    document.getElementById('uploadFolderName')?.focus();
  }
}

function toggleAsFolderUI() {
  const chk = document.getElementById('chkAsFolder');
  const divDetails = document.getElementById('divFolderDetails');
  const multiBadge = document.getElementById('multiAudioBadge');
  const btnText = document.getElementById('btnSubmitAudioUploadText');
  const singleFields = document.getElementById('singleAudioFields');

  populateTargetFolderSelect();

  if (chk && chk.checked) {
    if (divDetails) divDetails.classList.remove('hidden');
    if (multiBadge) multiBadge.classList.add('hidden');
    if (singleFields) singleFields.classList.add('hidden');
    if (btnText) btnText.innerText = `Simpan ke Folder Playlist (${selectedAudioFileList.length} Lagu)`;
    toggleFolderModeUI();
  } else {
    if (divDetails) divDetails.classList.add('hidden');
    if (selectedAudioFileList.length === 1) {
      if (singleFields) singleFields.classList.remove('hidden');
      if (multiBadge) multiBadge.classList.add('hidden');
      if (btnText) btnText.innerText = 'Upload 1 Lagu';
    } else {
      if (singleFields) singleFields.classList.add('hidden');
      if (multiBadge) multiBadge.classList.remove('hidden');
      if (btnText) btnText.innerText = `Upload ${selectedAudioFileList.length} Lagu Sekaligus`;
    }
  }
}

function renderSelectedAudioFiles() {
  const box = document.getElementById('selectedAudioPreviewBox');
  const countText = document.getElementById('selectedAudioCountText');
  const list = document.getElementById('selectedAudioList');
  const singleFields = document.getElementById('singleAudioFields');
  const multiBadge = document.getElementById('multiAudioBadge');
  const divAsFolder = document.getElementById('divAsFolderOption');
  const nameInput = document.getElementById('uploadAudioName');
  const btnText = document.getElementById('btnSubmitAudioUploadText');

  if (!box || !list) return;

  if (selectedAudioFileList.length === 0) {
    box.classList.add('hidden');
    if (singleFields) singleFields.classList.remove('hidden');
    if (multiBadge) multiBadge.classList.add('hidden');
    if (divAsFolder) divAsFolder.classList.add('hidden');
    if (btnText) btnText.innerText = 'Upload Lagu Sekarang';
    if (nameInput) nameInput.value = '';
    return;
  }

  box.classList.remove('hidden');
  countText.innerText = `📁 ${selectedAudioFileList.length} File Audio Dipilih:`;
  list.innerHTML = '';

  selectedAudioFileList.forEach((f, idx) => {
    const sizeMB = (f.size / (1024 * 1024)).toFixed(2);
    const item = document.createElement('div');
    item.className = 'py-1 flex items-center justify-between text-[11px] text-gray-700';
    item.innerHTML = `
      <span class="truncate max-w-[280px]" title="${escapeHtml(f.name)}">🎵 <b>${idx + 1}.</b> ${escapeHtml(f.name)}</span>
      <span class="text-gray-400 font-mono text-[10px] ml-2 flex-shrink-0">${sizeMB} MB</span>
    `;
    list.appendChild(item);
  });

  if (divAsFolder) divAsFolder.classList.remove('hidden');

  if (selectedAudioFileList.length === 1) {
    const chk = document.getElementById('chkAsFolder');
    if (!chk || !chk.checked) {
      if (singleFields) singleFields.classList.remove('hidden');
      if (multiBadge) multiBadge.classList.add('hidden');
      if (nameInput && !nameInput.value) {
        nameInput.value = selectedAudioFileList[0].name.replace(/\.[^/.]+$/, '');
      }
      if (btnText) btnText.innerText = 'Upload 1 Lagu';
    } else {
      toggleAsFolderUI();
    }
  } else {
    toggleAsFolderUI();
  }
}

async function submitAudioUpload(e) {
  e.preventDefault();
  if (!requireAdmin('mengunggah audio')) return;

  if (selectedAudioFileList.length === 0) {
    showToast('Pilih minimal 1 file audio (.mp3, .wav) terlebih dahulu!', 'warning');
    return;
  }

  const formData = new FormData();
  for (let i = 0; i < selectedAudioFileList.length; i++) {
    formData.append('audio_files', selectedAudioFileList[i]);
  }

  const isFolder = document.getElementById('chkAsFolder')?.checked;

  if (isFolder) {
    const isExisting = document.getElementById('radFolderModeExisting')?.checked;
    if (isExisting) {
      const targetCode = document.getElementById('selectTargetFolderCode')?.value;
      if (!targetCode) {
        showToast('Pilih folder playlist tujuan terlebih dahulu!', 'warning');
        return;
      }
      formData.append('target_folder_code', targetCode);
    } else {
      const folderName = document.getElementById('uploadFolderName')?.value.trim();
      if (!folderName) {
        showToast('Silakan ketik nama folder lagu acak terlebih dahulu!', 'warning');
        document.getElementById('uploadFolderName')?.focus();
        return;
      }
      formData.append('as_folder', 'true');
      formData.append('folder_name', folderName);
    }
  } else if (selectedAudioFileList.length === 1) {
    const name = document.getElementById('uploadAudioName')?.value.trim();
    const desc = document.getElementById('uploadAudioDesc')?.value.trim();
    if (name) formData.append('name', name);
    if (desc) formData.append('description', desc);
  }

  const btn = document.getElementById('btnSubmitAudioUpload');
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = `<span>⏳</span><span>Memproses ${selectedAudioFileList.length} lagu...</span>`;
  }

  try {
    const res = await authFetch('/api/audio/upload', {
      method: 'POST',
      body: formData
    });
    const json = await res.json();
    if (json.success) {
      if (json.data && json.data.is_folder) {
        showToast(`✅ ${json.data.messages || 'Folder Playlist berhasil diperbarui!'}`, 'success');
      } else {
        const cnt = json.data.count || 1;
        showToast(`✅ Berhasil mengunggah ${cnt} lagu audio baru!`, 'success');
      }
      closeModal('modalAudioUpload');
      clearSelectedAudioFiles();
      loadAudios();
      loadGroups();
    } else {
      showToast(json.error || 'Gagal mengunggah audio', 'error');
    }
  } catch (err) {
    if (err.message === 'Failed to fetch') {
      showToast('Gagal mengunggah file (Koneksi terputus / Sesi Admin berakhir). Silakan masukkan PIN Admin kembali lalu coba lagi.', 'error');
      handleAuthClick();
    } else {
      showToast('Gagal mengunggah ke server: ' + err.message, 'error');
    }
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = `<span>🚀</span><span id="btnSubmitAudioUploadText">Upload Lagu Sekarang</span>`;
    }
  }
}

async function deleteAudio(id) {
  if (!requireAdmin('menghapus audio')) return;
  if (!confirm('Hapus file audio ini?')) return;

  try {
    const res = await authFetch(`/api/audio/delete/${id}`, { method: 'DELETE' });
    const json = await res.json();
    if (json.success) {
      showToast(json.data, 'success');
      loadAudios();
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

// ==================== FOLDER SHUFFLE PLAYLIST MANAGEMENT ====================
let currentManageFolderCode = null;

async function openFolderManageModal(code, name) {
  currentManageFolderCode = code;
  const title = document.getElementById('folderManageTitle');
  const sub = document.getElementById('folderManageSubtitle');
  if (title) title.innerText = `Kelola Lagu: [${code}] ${name}`;
  if (sub) sub.innerText = `Daftar lagu di folder playlist [${code}]. Sistem akan memutar salah satu lagunya secara acak saat bel berbunyi.`;

  const fileInput = document.getElementById('folderManageFileInput');
  if (fileInput) fileInput.value = '';

  openModal('modalFolderManage');
  await reloadFolderSongs(code);
}

async function reloadFolderSongs(code) {
  const tbody = document.getElementById('folderManageTableBody');
  const countEl = document.getElementById('folderManageSongCount');
  if (!tbody) return;

  tbody.innerHTML = `<tr><td colspan="5" class="py-6 text-center text-gray-400">Memuat daftar lagu...</td></tr>`;

  try {
    const res = await fetch(`/api/audio/folder/${encodeURIComponent(code)}`);
    const json = await res.json();
    if (!json.success || !json.data) {
      tbody.innerHTML = `<tr><td colspan="5" class="py-6 text-center text-red-500">Gagal memuat isi folder</td></tr>`;
      return;
    }

    const files = json.data.files || [];
    if (countEl) countEl.innerText = `Total: ${files.length} lagu di folder ini`;

    if (files.length === 0) {
      tbody.innerHTML = `<tr><td colspan="5" class="py-6 text-center text-gray-400 italic">Belum ada lagu di dalam folder ini. Silakan unggah lagu di atas.</td></tr>`;
      return;
    }

    tbody.innerHTML = '';
    files.forEach((f, idx) => {
      const tr = document.createElement('tr');
      tr.className = 'hover:bg-amber-50/60 transition';
      tr.innerHTML = `
        <td class="py-2.5 px-3 text-center text-gray-500 font-mono">${idx + 1}</td>
        <td class="py-2.5 px-3 font-medium text-gray-800">
          <div class="truncate max-w-[280px]" title="${escapeHtml(f.name)}">🎵 ${escapeHtml(f.name)}</div>
        </td>
        <td class="py-2.5 px-3 text-center text-gray-500 font-mono text-[11px]">${f.size_fmt}</td>
        <td class="py-2.5 px-3 text-center">
          <button onclick="playSpecificFolderSong('${code}', '${escapeHtml(f.name)}')" class="px-2.5 py-1 bg-amber-200 hover:bg-amber-300 text-amber-900 rounded-lg text-xs font-semibold transition cursor-pointer">
            ▶ Dengar
          </button>
        </td>
        <td class="py-2.5 px-3 text-right">
          <button onclick="deleteFolderSong('${code}', '${escapeHtml(f.name)}')" class="text-red-600 hover:text-red-800 font-semibold text-xs transition cursor-pointer">
            Hapus
          </button>
        </td>
      `;
      tbody.appendChild(tr);
    });

  } catch (err) {
    tbody.innerHTML = `<tr><td colspan="5" class="py-6 text-center text-red-500">Terjadi kesalahan: ${err.message}</td></tr>`;
  }
}

async function uploadFilesToCurrentFolder() {
  if (!currentManageFolderCode) return;
  if (!requireAdmin('menambah lagu ke folder')) return;

  const input = document.getElementById('folderManageFileInput');
  if (!input || !input.files || input.files.length === 0) {
    showToast('Pilih minimal 1 file lagu (.mp3, .wav) terlebih dahulu!', 'warning');
    return;
  }

  const files = Array.from(input.files).filter(f => {
    const ext = f.name.toLowerCase();
    return ext.endsWith('.mp3') || ext.endsWith('.wav') || ext.endsWith('.ogg');
  });

  if (files.length === 0) {
    showToast('Format file harus .mp3, .wav, atau .ogg!', 'warning');
    return;
  }

  const btn = document.getElementById('btnUploadToFolder');
  if (btn) {
    btn.disabled = true;
    btn.innerText = `Mengunggah ${files.length} lagu...`;
  }

  const formData = new FormData();
  files.forEach(f => formData.append('audio_files', f));
  formData.append('target_folder_code', currentManageFolderCode);

  try {
    const res = await authFetch('/api/audio/upload', {
      method: 'POST',
      body: formData
    });
    const json = await res.json();
    if (json.success) {
      showToast(`✅ ${json.data.messages || 'Lagu berhasil ditambahkan!'}`, 'success');
      input.value = '';
      await reloadFolderSongs(currentManageFolderCode);
      loadAudios();
    } else {
      showToast(json.error || 'Gagal menambahkan lagu', 'error');
    }
  } catch (err) {
    showToast('Gagal mengunggah ke server: ' + err.message, 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.innerText = 'Unggah Lagu';
    }
  }
}

async function deleteFolderSong(code, fileName) {
  if (!requireAdmin('menghapus lagu dari folder')) return;
  if (!confirm(`Hapus lagu "${fileName}" dari folder ${code}?`)) return;

  try {
    const res = await authFetch(`/api/audio/folder/${encodeURIComponent(code)}/file?name=${encodeURIComponent(fileName)}`, {
      method: 'DELETE'
    });
    const json = await res.json();
    if (json.success) {
      showToast(json.data || 'Lagu berhasil dihapus dari folder', 'success');
      await reloadFolderSongs(code);
      loadAudios();
    } else {
      showToast(json.error || 'Gagal menghapus lagu', 'error');
    }
  } catch (err) {
    showToast('Gagal menghapus lagu: ' + err.message, 'error');
  }
}

async function playSpecificFolderSong(code, fileName) {
  stopAnyActivePlayback();

  activePlayback = {
    code: code,
    type: 'web',
    name: fileName
  };
  showFloatingPlayerBar('Browser Web (Preview Lagu)', `[${code}] ${fileName}`);

  audioPlayer.onended = () => {
    activePlayback = null;
    hideFloatingPlayerBar();
  };

  try {
    audioPlayer.src = `/api/audio/stream/${encodeURIComponent(code)}?file=${encodeURIComponent(fileName)}&t=${Date.now()}`;
    await audioPlayer.play();
  } catch (err) {
    console.warn('Playback interrupted:', err);
  }
}

// ==================== QUICK TRIGGERS & EMERGENCY ====================

async function triggerQuickBell(code, title) {
  if (!requireAdmin(`membunyikan ${title}`)) return;
  try {
    const res = await authFetch('/api/trigger-manual', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        audio_code: code,
        trigger_type: 'MANUAL',
        custom_title: title
      })
    });
    const json = await res.json();
    if (json.success) {
      showToast(`${title} dibunyikan!`, 'success');
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

function openEmergencyModal() {
  if (!requireAdmin('mengakses sirine darurat')) return;
  openModal('modalEmergency');
}

async function confirmEmergencyTrigger() {
  closeModal('modalEmergency');
  try {
    const res = await authFetch('/api/trigger-manual', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        audio_code: 'AUD-04',
        trigger_type: 'EMERGENCY',
        custom_title: '🚨 SIRINE DARURAT EVAKUASI'
      })
    });
    const json = await res.json();
    if (json.success) {
      showToast('SIRINE DARURAT DIBUNYIKAN!', 'error');
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

async function toggleHolidayMode() {
  if (!requireAdmin('mengubah mode libur')) return;
  try {
    const res = await authFetch('/api/toggle-holiday', { method: 'POST' });
    const json = await res.json();
    if (json.success) {
      showToast(json.data.holiday_mode ? 'Mode Libur DIAKTIFKAN' : 'Mode Libur DINONAKTIFKAN', 'info');
      loadSettings();
    }
  } catch (err) {}
}

// ==================== EXCEL IMPORT / EXPORT ====================

async function downloadTemplateExcel() {
  const select = document.getElementById('templateTargetGroupSelect');
  const val = select ? select.value : '';
  let url = '/api/excel/template.xlsx';
  if (val) {
    url += '?group_id=' + encodeURIComponent(val);
  }

  showToast('Menyiapkan file template Excel...', 'info');

  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error('Gagal mengunduh template');

    let filename = 'Template_Jadwal_Bel_Budut.xlsx';
    const disp = res.headers.get('Content-Disposition');
    if (disp && disp.includes('filename=')) {
      const match = disp.match(/filename="?([^";]+)"?/);
      if (match && match[1]) filename = match[1];
    }

    const blob = await res.blob();
    const blobUrl = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = blobUrl;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    window.URL.revokeObjectURL(blobUrl);

    showToast('Template Excel (.xlsx) berhasil diunduh!', 'success');
  } catch (err) {
    // Fallback direct link
    window.location.href = url;
  }
}

async function exportSchedulesExcel() {
  const select = document.getElementById('exportGroupSelect');
  const val = select ? select.value : 'all';
  const url = '/api/excel/export.xlsx?group_id=' + encodeURIComponent(val);

  showToast('Menyiapkan file ekspor Excel...', 'info');

  try {
    const res = await fetch(url);
    if (!res.ok) throw new Error('Gagal mengekspor jadwal');

    let filename = 'Jadwal_Bel_Budut.xlsx';
    const disp = res.headers.get('Content-Disposition');
    if (disp && disp.includes('filename=')) {
      const match = disp.match(/filename="?([^";]+)"?/);
      if (match && match[1]) filename = match[1];
    }

    const blob = await res.blob();
    const blobUrl = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = blobUrl;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    window.URL.revokeObjectURL(blobUrl);

    showToast('Jadwal Excel (.xlsx) berhasil diunduh!', 'success');
  } catch (err) {
    // Fallback direct link
    window.location.href = url;
  }
}

function openExcelImportModal() {
  if (!requireAdmin('mengimpor Excel')) return;
  const box = document.getElementById('importReportBox');
  if (box) {
    box.classList.add('hidden');
    box.innerHTML = '';
  }
  const fileInput = document.getElementById('uploadExcelFile');
  if (fileInput) fileInput.value = '';

  const rSpecific = document.getElementById('grpModeSpecific');
  if (rSpecific) rSpecific.checked = true;
  updateImportModalGroupUI();

  openModal('modalExcelImport');
}

function updateImportModalGroupUI() {
  const mode = document.querySelector('input[name="import_group_mode"]:checked')?.value || 'specific';
  const divSpecific = document.getElementById('divTargetGroupSelect');
  const divNew = document.getElementById('divNewGroupName');

  if (divSpecific) {
    if (mode === 'specific') {
      divSpecific.classList.remove('hidden');
    } else {
      divSpecific.classList.add('hidden');
    }
  }
  if (divNew) {
    if (mode === 'new') {
      divNew.classList.remove('hidden');
      const input = document.getElementById('importNewGroupNameInput');
      if (input) input.focus();
    } else {
      divNew.classList.add('hidden');
    }
  }
}

async function submitExcelImport(e) {
  e.preventDefault();
  if (!requireAdmin('mengimpor Excel')) return;

  const fileInput = document.getElementById('uploadExcelFile');
  if (!fileInput || !fileInput.files || fileInput.files.length === 0) {
    showToast('Pilih file Excel (.xlsx) terlebih dahulu!', 'warning');
    return;
  }

  const groupMode = document.querySelector('input[name="import_group_mode"]:checked')?.value || 'file';
  const targetGroupID = document.getElementById('importTargetGroupSelect')?.value || '';
  const newGroupName = document.getElementById('importNewGroupNameInput')?.value.trim() || '';
  const replaceMode = document.querySelector('input[name="import_replace_mode"]:checked')?.value || 'replace';

  if (groupMode === 'new' && !newGroupName) {
    showToast('Silakan masukkan nama grup baru terlebih dahulu!', 'warning');
    document.getElementById('importNewGroupNameInput')?.focus();
    return;
  }

  if (groupMode === 'specific' && !targetGroupID) {
    showToast('Silakan pilih grup tujuan!', 'warning');
    return;
  }

  if (groupMode === 'specific' && replaceMode === 'replace') {
    const selOpt = document.getElementById('importTargetGroupSelect')?.selectedOptions[0]?.text || 'grup yang dipilih';
    if (!confirm(`PERINGATAN: Opsi 'Timpa Total' akan menghapus seluruh jadwal lama di [${selOpt}] dan menggantinya dengan isi file Excel ini. Lanjutkan?`)) {
      return;
    }
  }

  const formData = new FormData();
  formData.append('excel_file', fileInput.files[0]);
  formData.append('group_mode', groupMode);
  formData.append('target_group_id', targetGroupID);
  formData.append('new_group_name', newGroupName);
  formData.append('mode', replaceMode);

  const btn = document.getElementById('btnSubmitExcelImport');
  const originalBtnText = btn ? btn.innerHTML : '';
  if (btn) {
    btn.disabled = true;
    btn.innerHTML = `<span>⏳</span><span>Memproses Excel...</span>`;
  }

  const box = document.getElementById('importReportBox');
  if (box) {
    box.classList.remove('hidden');
    box.innerHTML = `<div class="text-blue-700 font-semibold animate-pulse">⏳ Sedang membaca dan memvalidasi file Excel...</div>`;
  }

  try {
    const res = await authFetch('/api/excel/import', {
      method: 'POST',
      body: formData
    });
    const json = await res.json();

    if (json.success) {
      const rep = json.data;
      let msgHtml = '';
      if (rep.messages && rep.messages.length > 0) {
        msgHtml = `<div class="mt-2 space-y-1 max-h-36 overflow-y-auto border-t border-gray-200 pt-2 text-[11px] text-gray-600">
          ${rep.messages.map(m => `<div>• ${m}</div>`).join('')}
        </div>`;
      }
      box.innerHTML = `
        <div class="font-bold text-green-800 flex items-center space-x-1">
          <span>✅</span>
          <span>Berhasil mengimpor ${rep.success_count} jadwal (Total diproses: ${rep.total_processed} baris).</span>
        </div>
        ${msgHtml}
      `;
      showToast(`Sukses mengimpor ${rep.success_count} jadwal!`, 'success');
      loadSchedules();
      loadGroups();
    } else {
      box.innerHTML = `<div class="text-red-700 font-bold">❌ Gagal import: ${json.error || 'Terjadi kesalahan sistem'}</div>`;
      showToast(json.error || 'Gagal import Excel', 'error');
    }
  } catch (err) {
    if (box) {
      box.innerHTML = `<div class="text-red-700 font-bold">❌ Terjadi kesalahan: ${err.message}</div>`;
    }
    showToast('Terjadi kesalahan saat mengunggah file', 'error');
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.innerHTML = originalBtnText;
    }
  }
}

// ==================== BACKUP & RESTORE ====================

async function downloadBackup() {
  if (!requireAdmin('mengunduh cadangan database')) return;
  showToast('Menyiapkan file cadangan database (.db)...', 'info');
  try {
    const res = await authFetch('/api/backup/download');
    if (!res.ok) throw new Error('Gagal mengunduh backup');
    let filename = 'backup_bell_budut.db';
    const disp = res.headers.get('Content-Disposition');
    if (disp && disp.includes('filename=')) {
      const match = disp.match(/filename="?([^";]+)"?/);
      if (match && match[1]) filename = match[1];
    }
    const blob = await res.blob();
    const blobUrl = window.URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = blobUrl;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    window.URL.revokeObjectURL(blobUrl);
    showToast('Database berhasil di-backup (.db)!', 'success');
  } catch (err) {
    window.location.href = '/api/backup/download';
  }
}

function openRestoreModal() {
  if (!requireAdmin('merestore database')) return;
  openModal('modalRestore');
}

async function submitRestoreDB(e) {
  e.preventDefault();
  if (!confirm('PERINGATAN: Memulihkan database akan menimpa seluruh jadwal saat ini dengan data dari file backup. Lanjutkan?')) return;

  const form = document.getElementById('formRestoreDB');
  const formData = new FormData(form);

  try {
    const res = await authFetch('/api/backup/restore', {
      method: 'POST',
      body: formData
    });
    const json = await res.json();
    if (json.success) {
      showToast('Database berhasil dipulihkan!', 'success');
      closeModal('modalRestore');
      loadGroups();
      loadAudios();
      loadSchedules();
      loadSettings();
      loadLogs();
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

async function loadLogs() {
  try {
    const res = await fetch('/api/logs?limit=30');
    const json = await res.json();
    if (!json.success) return;

    const tbody = document.getElementById('logsTableBody');
    tbody.innerHTML = '';
    const logs = json.data || [];

    if (logs.length === 0) {
      tbody.innerHTML = '<tr><td colspan="6" class="py-4 text-center text-gray-400">Belum ada riwayat bel.</td></tr>';
      return;
    }

    logs.forEach(l => {
      const tr = document.createElement('tr');
      tr.className = 'hover:bg-gray-50';
      tr.innerHTML = `
        <td class="py-2.5 px-3 font-mono text-gray-500">${l.triggered_at}</td>
        <td class="py-2.5 px-3 font-bold text-gray-800">${l.title}</td>
        <td class="py-2.5 px-3 text-gray-600">${l.group_name || '-'}</td>
        <td class="py-2.5 px-3 font-mono text-green-700">${l.audio_code || '-'}</td>
        <td class="py-2.5 px-3">
          <span class="px-2 py-0.5 rounded text-xs font-semibold ${
            l.trigger_type === 'EMERGENCY' ? 'bg-red-100 text-red-800' :
            l.trigger_type === 'MANUAL' ? 'bg-yellow-100 text-yellow-800' : 'bg-blue-100 text-blue-800'
          }">${l.trigger_type}</span>
        </td>
        <td class="py-2.5 px-3 text-center">
          <span class="px-2 py-0.5 rounded text-xs font-semibold ${
            l.status === 'SUCCESS' ? 'bg-green-100 text-green-800' :
            l.status === 'SKIPPED_HOLIDAY' ? 'bg-gray-100 text-gray-600' : 'bg-red-100 text-red-800'
          }">${l.status}</span>
        </td>
      `;
      tbody.appendChild(tr);
    });

  } catch (err) {
    console.error('Failed to load logs:', err);
  }
}

// ==================== SETTINGS ====================

async function loadSettings() {
  try {
    const res = await fetch('/api/settings');
    const json = await res.json();
    if (!json.success) return;

    const s = json.data || {};
    document.getElementById('headerSchoolName').innerText = s.school_name || 'SMK BUDI UTOMO WAY JEPARA';
    document.getElementById('settingSchoolName').value = s.school_name || 'SMK BUDI UTOMO WAY JEPARA';
    document.getElementById('settingServerAudio').checked = s.server_audio_enabled !== 'false';
    document.getElementById('settingHolidayMode').checked = s.holiday_mode === 'true';

    // Load Windows Auto-Start status
    loadAutoStartStatus();
  } catch (err) {}
}

async function loadAutoStartStatus() {
  try {
    const res = await fetch('/api/autostart');
    const json = await res.json();
    if (json.success && json.data.supported) {
      document.getElementById('settingAutoStart').checked = json.data.enabled;
    }
  } catch (err) {}
}

async function toggleAutoStart(enabled) {
  if (!requireAdmin('mengubah pengaturan Auto-Start Windows')) {
    // Revert checkbox if unauthorized
    document.getElementById('settingAutoStart').checked = !enabled;
    return;
  }

  try {
    const res = await authFetch('/api/autostart', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled })
    });
    const json = await res.json();
    if (json.success) {
      showToast(json.data.message || (enabled ? 'Auto-Start DIAKTIFKAN' : 'Auto-Start DINONAKTIFKAN'), 'success');
      loadAutoStartStatus();
    } else {
      document.getElementById('settingAutoStart').checked = !enabled;
      showToast(json.error, 'error');
    }
  } catch (err) {
    document.getElementById('settingAutoStart').checked = !enabled;
  }
}

async function saveSettings() {
  if (!requireAdmin('menyimpan pengaturan')) return;

  const payload = {
    school_name: document.getElementById('settingSchoolName').value.trim(),
    server_audio_enabled: document.getElementById('settingServerAudio').checked ? 'true' : 'false',
    holiday_mode: document.getElementById('settingHolidayMode').checked ? 'true' : 'false'
  };

  try {
    const res = await authFetch('/api/settings', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    const json = await res.json();
    if (json.success) {
      showToast('Pengaturan berhasil disimpan', 'success');
      loadSettings();
    }
  } catch (err) {}
}

async function submitChangePIN() {
  if (!requireAdmin('mengubah PIN')) return;
  const old_pin = document.getElementById('inputOldPIN').value.trim();
  const new_pin = document.getElementById('inputNewPIN').value.trim();

  if (!old_pin || !new_pin) {
    showToast('PIN lama dan PIN baru harus diisi', 'warning');
    return;
  }

  try {
    const res = await authFetch('/api/change-pin', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ old_pin, new_pin })
    });
    const json = await res.json();
    if (json.success) {
      showToast('PIN Admin berhasil diperbarui!', 'success');
      document.getElementById('inputOldPIN').value = '';
      document.getElementById('inputNewPIN').value = '';
    } else {
      showToast(json.error, 'error');
    }
  } catch (err) {}
}

// ==================== UI HELPERS & MODALS ====================

function openModal(id) {
  document.getElementById(id).classList.remove('hidden');
}

function closeModal(id) {
  document.getElementById(id).classList.add('hidden');
}

function showToast(message, type = 'info') {
  const container = document.getElementById('toastContainer');
  if (!container) return;
  const toast = document.createElement('div');

  // Solid, rich, non-transparent toast styling with high contrast & shadow
  let bgColor = '#1e3a8a'; // Blue 900
  let borderColor = '#3b82f6'; // Blue 500
  let icon = 'ℹ️';

  if (type === 'success') {
    bgColor = '#14532d'; // Green 900
    borderColor = '#22c55e'; // Green 500
    icon = '✅';
  } else if (type === 'warning') {
    bgColor = '#78350f'; // Amber 900
    borderColor = '#f59e0b'; // Amber 500
    icon = '⚠️';
  } else if (type === 'error') {
    bgColor = '#7f1d1d'; // Red 900
    borderColor = '#ef4444'; // Red 500
    icon = '❌';
  }

  toast.style.cssText = `
    background-color: ${bgColor} !important;
    color: #ffffff !important;
    border: 1.5px solid ${borderColor} !important;
    padding: 12px 18px !important;
    border-radius: 14px !important;
    box-shadow: 0 10px 25px -3px rgba(0, 0, 0, 0.6), 0 4px 6px -4px rgba(0, 0, 0, 0.4) !important;
    font-size: 13px !important;
    font-weight: 700 !important;
    display: flex !important;
    align-items: center !important;
    gap: 12px !important;
    pointer-events: auto !important;
    transform: translateY(12px) !important;
    opacity: 0 !important;
    transition: all 0.3s cubic-bezier(0.16, 1, 0.3, 1) !important;
    z-index: 999999 !important;
  `;
  toast.innerHTML = `<span style="font-size: 18px; flex-shrink: 0;">${icon}</span><span style="flex: 1; line-height: 1.4; color: #ffffff !important;">${message}</span>`;
  container.appendChild(toast);

  setTimeout(() => {
    toast.style.transform = 'translateY(0)';
    toast.style.opacity = '1';
  }, 10);

  setTimeout(() => {
    toast.style.opacity = '0';
    toast.style.transform = 'translateY(12px)';
    setTimeout(() => toast.remove(), 350);
  }, 3800);
}


// ==================== WINDOWS AUDIO ISOLATOR & MIXER HELPERS ====================

async function openWindowsVolumeMixer() {
  try {
    const res = await fetch('/api/system/open-volume-mixer', { method: 'POST' });
    const json = await res.json();
    if (json.success) {
      showToast('Pengatur Suara Aplikasi Windows dibuka di layar', 'success');
    } else {
      showToast(json.error || 'Gagal membuka pengatur suara', 'error');
    }
  } catch (err) {
    showToast('Gagal membuka pengatur suara', 'error');
  }
}

async function openWindowsSndvol() {
  try {
    const res = await fetch('/api/system/open-sndvol', { method: 'POST' });
    const json = await res.json();
    if (json.success) {
      showToast('Volume Mixer Windows klasik dibuka di layar', 'success');
    } else {
      showToast(json.error || 'Gagal membuka Volume Mixer', 'error');
    }
  } catch (err) {
    showToast('Gagal membuka Volume Mixer', 'error');
  }
}

async function loadWindowsSoundsStatus() {
  try {
    const res = await fetch('/api/system/windows-sounds');
    const json = await res.json();
    if (json.success && json.data) {
      const chk1 = document.getElementById('settingMuteWinSounds');
      const chk2 = document.getElementById('modalMuteWinSounds');
      if (chk1) chk1.checked = json.data.is_muted;
      if (chk2) chk2.checked = json.data.is_muted;
    }
  } catch (err) {}
}

async function toggleWindowsSoundsMute(mute) {
  try {
    const res = await fetch('/api/system/windows-sounds', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mute })
    });
    const json = await res.json();
    if (json.success) {
      showToast(json.data.message, 'success');
      const chk1 = document.getElementById('settingMuteWinSounds');
      const chk2 = document.getElementById('modalMuteWinSounds');
      if (chk1) chk1.checked = mute;
      if (chk2) chk2.checked = mute;
    } else {
      showToast(json.error || 'Gagal mengubah pengaturan suara Windows', 'error');
    }
  } catch (err) {
    showToast('Gagal mengubah pengaturan suara Windows', 'error');
  }
}

