'use strict';

const roleLabels = {viewer: 'Наблюдатель', operator: 'Оператор', admin: 'Администратор'};
const scopeLabels = {servers: 'Только серверы', infrastructure: 'Серверы и коммутаторы', all: 'Все разделы'};
const roleHints = {
  viewer: 'Просмотр данных в доступных разделах.',
  operator: 'Просмотр и настройка коммутаторов и местоположения доступного оборудования. При доступе ко всем разделам можно менять общий обзор и схемы.',
  admin: 'Полные права, включая управление пользователями, SMTP и Syslog.'
};
let accountEditing = '';

function resetUserForm() {
  $('user-form').reset();
  $('user-form').classList.add('hidden');
  $('user-name').readOnly = false;
  $('user-scope').disabled = false;
  $('user-password').required = true;
  accountEditing = '';
}

async function loadAccounts() {
  try {
    const accounts = await api('/api/settings/users');
    if (!session || currentRoute.page !== 'settings' || currentRoute.id !== 'users') return;
    $('users-list').replaceChildren(dataTable(['Логин', 'Роль', 'Доступ', 'Действия'], accounts.map(account => {
      const actions = document.createElement('div');
      actions.className = 'toolbar-actions';
      if (!account.owner && account.username !== activeAccount.username) {
        actions.append(action('Изменить', () => editAccount(account)), action('Удалить', () => deleteAccount(account)));
      }
      return [account.username + (account.owner ? ' · владелец' : ''), roleLabels[account.role], scopeLabels[account.scope], actions];
    })));
  } catch (error) {
    msg('users-msg', error.message, true);
  }
}

function editAccount(account) {
  accountEditing = account?.username || '';
  $('user-name').value = accountEditing;
  $('user-name').readOnly = !!account;
  $('user-password').value = '';
  $('user-password').required = !account;
  $('user-password').placeholder = account ? 'Оставьте пустым, чтобы сохранить пароль' : '';
  $('user-role').value = account?.role || 'viewer';
  $('user-scope').value = account?.scope || 'servers';
  $('user-save').textContent = account ? 'Сохранить изменения' : 'Создать';
  syncAccountRole();
  msg('users-msg');
  $('user-form').classList.remove('hidden');
  $(account ? 'user-role' : 'user-name').focus();
}

function syncAccountRole() {
  const role = $('user-role').value;
  $('user-scope').disabled = role === 'admin';
  if (role === 'admin') $('user-scope').value = 'all';
  $('user-role-hint').textContent = roleHints[role];
}

async function deleteAccount(account) {
  if (!confirm('Удалить пользователя «' + account.username + '»?')) return;
  try {
    await api('/api/settings/users/' + encodeURIComponent(account.username), {method: 'DELETE'});
    if (accountEditing === account.username) resetUserForm();
    msg('users-msg', 'Пользователь удалён.');
    await loadAccounts();
  } catch (error) {
    msg('users-msg', error.message, true);
  }
}

$('user-add').addEventListener('click', () => editAccount());
$('user-role').addEventListener('change', syncAccountRole);
$('user-cancel').addEventListener('click', resetUserForm);
$('user-form').addEventListener('submit', async event => {
  event.preventDefault();
  const editing = accountEditing;
  $('user-save').disabled = true;
  try {
    await api('/api/settings/users' + (editing ? '/' + encodeURIComponent(editing) : ''), {
      method: editing ? 'PUT' : 'POST',
      body: JSON.stringify({username: $('user-name').value.trim(), password: $('user-password').value,
        role: $('user-role').value, scope: $('user-scope').value})
    });
    resetUserForm();
    msg('users-msg', editing ? 'Права пользователя обновлены.' : 'Пользователь создан.');
    await loadAccounts();
  } catch (error) {
    msg('users-msg', error.message, true);
  } finally {
    $('user-save').disabled = false;
  }
});
