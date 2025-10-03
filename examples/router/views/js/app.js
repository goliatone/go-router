/**
 * go-router Demo Application
 * Client-side JavaScript for user management interface
 */

// ======================
// Utility Functions
// ======================

const Utils = {
    /**
     * Debounce function execution
     */
    debounce(func, wait) {
        let timeout;
        return function executedFunction(...args) {
            const later = () => {
                clearTimeout(timeout);
                func(...args);
            };
            clearTimeout(timeout);
            timeout = setTimeout(later, wait);
        };
    },

    /**
     * Format date to relative time (e.g., "2 hours ago")
     */
    getRelativeTime(timestamp) {
        const now = new Date();
        const past = new Date(timestamp);
        const diffMs = now - past;
        const diffSecs = Math.floor(diffMs / 1000);
        const diffMins = Math.floor(diffSecs / 60);
        const diffHours = Math.floor(diffMins / 60);
        const diffDays = Math.floor(diffHours / 24);

        if (diffSecs < 60) return 'just now';
        if (diffMins < 60) return `${diffMins} minute${diffMins > 1 ? 's' : ''} ago`;
        if (diffHours < 24) return `${diffHours} hour${diffHours > 1 ? 's' : ''} ago`;
        if (diffDays < 7) return `${diffDays} day${diffDays > 1 ? 's' : ''} ago`;
        if (diffDays < 30) return `${Math.floor(diffDays / 7)} week${Math.floor(diffDays / 7) > 1 ? 's' : ''} ago`;
        if (diffDays < 365) return `${Math.floor(diffDays / 30)} month${Math.floor(diffDays / 30) > 1 ? 's' : ''} ago`;
        return `${Math.floor(diffDays / 365)} year${Math.floor(diffDays / 365) > 1 ? 's' : ''} ago`;
    },

    /**
     * Copy text to clipboard
     */
    async copyToClipboard(text) {
        try {
            await navigator.clipboard.writeText(text);
            return true;
        } catch (err) {
            console.error('Failed to copy:', err);
            return false;
        }
    },

    /**
     * Show loading overlay
     */
    showLoading() {
        const overlay = document.getElementById('loading-overlay');
        if (overlay) overlay.style.display = 'flex';
    },

    /**
     * Hide loading overlay
     */
    hideLoading() {
        const overlay = document.getElementById('loading-overlay');
        if (overlay) overlay.style.display = 'none';
    }
};

// ======================
// Flash Messages
// ======================

const FlashMessages = {
    init() {
        // Auto-dismiss flash messages after 5 seconds
        const flashes = document.querySelectorAll('.flash');
        flashes.forEach(flash => {
            setTimeout(() => {
                this.dismiss(flash);
            }, 5000);
        });

        // Close button handlers
        document.querySelectorAll('.flash-close').forEach(btn => {
            btn.addEventListener('click', (e) => {
                const flash = e.target.closest('.flash');
                this.dismiss(flash);
            });
        });
    },

    dismiss(flash) {
        flash.style.animation = 'slideOut 0.3s ease-in';
        setTimeout(() => {
            flash.remove();
        }, 300);
    },

    show(type, message) {
        const container = document.getElementById('flash-container');
        if (!container) return;

        const icons = {
            success: '✓',
            error: '✕',
            warning: '⚠',
            info: 'ℹ'
        };

        const flash = document.createElement('div');
        flash.className = `flash flash-${type}`;
        flash.innerHTML = `
            <span class="flash-icon">${icons[type] || 'ℹ'}</span>
            <span class="flash-message">${message}</span>
            <button class="flash-close" aria-label="Close">&times;</button>
        `;

        container.appendChild(flash);

        // Add event listener to close button
        flash.querySelector('.flash-close').addEventListener('click', () => {
            this.dismiss(flash);
        });

        // Auto-dismiss
        setTimeout(() => {
            this.dismiss(flash);
        }, 5000);
    }
};

// Add CSS for slideOut animation
const style = document.createElement('style');
style.textContent = `
    @keyframes slideOut {
        from {
            transform: translateX(0);
            opacity: 1;
        }
        to {
            transform: translateX(400px);
            opacity: 0;
        }
    }
`;
document.head.appendChild(style);

// ======================
// Modal
// ======================

const Modal = {
    element: null,
    overlay: null,
    confirmCallback: null,

    init() {
        this.element = document.getElementById('confirm-modal');
        if (!this.element) return;

        this.overlay = this.element.querySelector('.modal-overlay');

        // Close on overlay click
        this.overlay.addEventListener('click', () => this.hide());

        // Cancel button
        document.getElementById('modal-cancel')?.addEventListener('click', () => this.hide());

        // Confirm button
        document.getElementById('modal-confirm')?.addEventListener('click', () => {
            if (this.confirmCallback) {
                this.confirmCallback();
            }
            this.hide();
        });

        // ESC key to close
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && this.element.style.display !== 'none') {
                this.hide();
            }
        });
    },

    show(title, message, onConfirm) {
        if (!this.element) return;

        document.getElementById('modal-title').textContent = title;
        document.getElementById('modal-message').textContent = message;
        this.confirmCallback = onConfirm;

        this.element.style.display = 'flex';
        document.body.style.overflow = 'hidden';
    },

    hide() {
        if (!this.element) return;

        this.element.style.display = 'none';
        document.body.style.overflow = '';
        this.confirmCallback = null;
    }
};

// ======================
// User List
// ======================

const UserList = {
    searchInput: null,
    tableBody: null,
    cardsContainer: null,
    noResults: null,
    resultsCount: null,
    allRows: [],
    currentSort: { column: null, direction: 'asc' },

    init() {
        this.searchInput = document.getElementById('user-search');
        this.tableBody = document.getElementById('user-table-body');
        this.cardsContainer = document.querySelector('.cards-container');
        this.noResults = document.getElementById('no-results');
        this.resultsCount = document.getElementById('results-count');

        // Store all user rows
        this.allRows = Array.from(document.querySelectorAll('.user-row, .user-card'));

        // Search functionality
        if (this.searchInput) {
            this.searchInput.addEventListener('input',
                Utils.debounce(() => this.handleSearch(), 300)
            );
        }

        // Sort functionality
        document.querySelectorAll('.sortable').forEach(th => {
            th.addEventListener('click', () => this.handleSort(th));
        });

        // Delete buttons
        document.querySelectorAll('.delete-user').forEach(btn => {
            btn.addEventListener('click', (e) => this.handleDelete(e));
        });

        // Update relative timestamps
        this.updateRelativeTimes();
        setInterval(() => this.updateRelativeTimes(), 60000); // Update every minute
    },

    handleSearch() {
        const query = this.searchInput.value.toLowerCase().trim();
        let visibleCount = 0;

        this.allRows.forEach(row => {
            const name = row.dataset.userName?.toLowerCase() || '';
            const email = row.dataset.userEmail?.toLowerCase() || '';
            const matches = name.includes(query) || email.includes(query);

            row.style.display = matches ? '' : 'none';
            if (matches) visibleCount++;
        });

        // Show/hide no results message
        if (this.noResults) {
            this.noResults.style.display = visibleCount === 0 && query ? 'block' : 'none';
        }

        // Update results count
        if (this.resultsCount) {
            this.resultsCount.textContent = `${visibleCount} user${visibleCount !== 1 ? 's' : ''}`;
        }
    },

    handleSort(th) {
        const column = th.dataset.sort;

        // Update sort direction
        if (this.currentSort.column === column) {
            this.currentSort.direction = this.currentSort.direction === 'asc' ? 'desc' : 'asc';
        } else {
            this.currentSort.column = column;
            this.currentSort.direction = 'asc';
        }

        // Update UI
        document.querySelectorAll('.sortable').forEach(header => {
            header.classList.remove('sort-asc', 'sort-desc');
        });
        th.classList.add(`sort-${this.currentSort.direction}`);

        // Sort rows
        const sortedRows = this.sortRows(column, this.currentSort.direction);

        // Re-append in sorted order
        if (this.tableBody) {
            sortedRows.forEach(row => {
                if (row.classList.contains('user-row')) {
                    this.tableBody.appendChild(row);
                }
            });
        }
        if (this.cardsContainer) {
            sortedRows.forEach(row => {
                if (row.classList.contains('user-card')) {
                    this.cardsContainer.appendChild(row);
                }
            });
        }
    },

    sortRows(column, direction) {
        return this.allRows.sort((a, b) => {
            let aValue, bValue;

            switch (column) {
                case 'name':
                    aValue = a.dataset.userName || '';
                    bValue = b.dataset.userName || '';
                    break;
                case 'email':
                    aValue = a.dataset.userEmail || '';
                    bValue = b.dataset.userEmail || '';
                    break;
                case 'created':
                case 'updated':
                    const aElem = a.querySelector(`.user-${column}`);
                    const bElem = b.querySelector(`.user-${column}`);
                    aValue = aElem?.dataset.timestamp || '';
                    bValue = bElem?.dataset.timestamp || '';
                    break;
                default:
                    return 0;
            }

            const comparison = aValue.localeCompare(bValue);
            return direction === 'asc' ? comparison : -comparison;
        });
    },

    async handleDelete(e) {
        const btn = e.target.closest('.delete-user');
        const userId = btn.dataset.userId;
        const userName = btn.dataset.userName;
        const redirect = btn.dataset.redirect;

        Modal.show(
            'Delete User',
            `Are you sure you want to delete "${userName}"? This action cannot be undone.`,
            async () => {
                try {
                    Utils.showLoading();

                    const response = await fetch(`/api/users/${userId}`, {
                        method: 'DELETE'
                    });

                    if (response.ok || response.status === 204) {
                        // If redirect is specified, go there
                        if (redirect) {
                            window.location.href = redirect;
                        } else {
                            // Remove from DOM
                            const row = document.querySelector(`.user-row[data-user-id="${userId}"], .user-card[data-user-id="${userId}"]`);
                            if (row) {
                                row.style.animation = 'fadeOut 0.3s ease-out';
                                setTimeout(() => {
                                    row.remove();
                                    this.allRows = this.allRows.filter(r => r !== row);
                                    this.updateResultsCount();
                                }, 300);
                            }

                            FlashMessages.show('success', `User "${userName}" has been deleted successfully`);
                        }
                    } else {
                        const error = await response.json().catch(() => ({ message: 'Failed to delete user' }));
                        FlashMessages.show('error', error.message || 'Failed to delete user');
                    }
                } catch (err) {
                    console.error('Delete error:', err);
                    FlashMessages.show('error', 'Network error occurred while deleting user');
                } finally {
                    Utils.hideLoading();
                }
            }
        );
    },

    updateRelativeTimes() {
        document.querySelectorAll('.detail-time-relative').forEach(elem => {
            const timestamp = elem.dataset.timestamp;
            if (timestamp) {
                elem.textContent = `(${Utils.getRelativeTime(timestamp)})`;
            }
        });
    },

    updateResultsCount() {
        const visibleRows = this.allRows.filter(row => row.style.display !== 'none');
        if (this.resultsCount) {
            this.resultsCount.textContent = `${visibleRows.length} user${visibleRows.length !== 1 ? 's' : ''}`;
        }

        // Show empty state if no users
        if (visibleRows.length === 0 && !this.searchInput?.value) {
            const emptyState = document.querySelector('.empty-state');
            if (emptyState) {
                emptyState.style.display = 'block';
            }
        }
    }
};

// Add fadeOut animation
const fadeOutStyle = document.createElement('style');
fadeOutStyle.textContent = `
    @keyframes fadeOut {
        from {
            opacity: 1;
            transform: translateX(0);
        }
        to {
            opacity: 0;
            transform: translateX(-20px);
        }
    }
`;
document.head.appendChild(fadeOutStyle);

// ======================
// User Form
// ======================

const UserForm = {
    form: null,
    submitBtn: null,
    mode: 'create',

    init(mode = 'create') {
        this.mode = mode;
        this.form = document.getElementById('user-form');
        this.submitBtn = document.getElementById('submit-btn');

        if (!this.form) return;

        // Form validation on submit
        this.form.addEventListener('submit', (e) => this.handleSubmit(e));

        // Real-time field validation
        const nameInput = document.getElementById('name');
        const emailInput = document.getElementById('email');

        if (nameInput) {
            nameInput.addEventListener('blur', () => this.validateField('name'));
            nameInput.addEventListener('input', () => this.clearFieldError('name'));
        }

        if (emailInput && this.mode === 'create') {
            emailInput.addEventListener('blur', () => this.validateField('email'));
            emailInput.addEventListener('input', () => this.clearFieldError('email'));
        }
    },

    validateField(fieldName) {
        const input = document.getElementById(fieldName);
        const errorElem = document.getElementById(`${fieldName}-error`);

        if (!input || !errorElem) return true;

        let isValid = true;
        let errorMessage = '';

        if (fieldName === 'name') {
            if (!input.value.trim()) {
                isValid = false;
                errorMessage = 'Name is required';
            } else if (input.value.trim().length < 1) {
                isValid = false;
                errorMessage = 'Name must be at least 1 character';
            }
        }

        if (fieldName === 'email') {
            const emailPattern = /^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$/i;
            if (!input.value.trim()) {
                isValid = false;
                errorMessage = 'Email is required';
            } else if (!emailPattern.test(input.value)) {
                isValid = false;
                errorMessage = 'Please enter a valid email address';
            }
        }

        if (!isValid) {
            input.classList.add('invalid');
            errorElem.textContent = errorMessage;
            errorElem.style.display = 'block';
        } else {
            input.classList.remove('invalid');
            errorElem.textContent = '';
            errorElem.style.display = 'none';
        }

        return isValid;
    },

    clearFieldError(fieldName) {
        const input = document.getElementById(fieldName);
        const errorElem = document.getElementById(`${fieldName}-error`);

        if (input && input.classList.contains('invalid') && input.value.trim()) {
            input.classList.remove('invalid');
            if (errorElem) {
                errorElem.textContent = '';
                errorElem.style.display = 'none';
            }
        }
    },

    async handleSubmit(e) {
        e.preventDefault();

        // Validate all fields
        const nameValid = this.validateField('name');
        const emailValid = this.mode === 'create' ? this.validateField('email') : true;

        if (!nameValid || !emailValid) {
            FlashMessages.show('error', 'Please correct the errors in the form');
            return;
        }

        // Show loading state
        this.setSubmitting(true);

        // Submit form normally (not AJAX) to handle server-side redirects
        this.form.submit();
    },

    setSubmitting(isSubmitting) {
        if (!this.submitBtn) return;

        const btnText = this.submitBtn.querySelector('.btn-text');
        const btnSpinner = this.submitBtn.querySelector('.btn-spinner');

        if (isSubmitting) {
            this.submitBtn.disabled = true;
            if (btnText) btnText.style.display = 'none';
            if (btnSpinner) btnSpinner.style.display = 'inline-block';
        } else {
            this.submitBtn.disabled = false;
            if (btnText) btnText.style.display = 'inline';
            if (btnSpinner) btnSpinner.style.display = 'none';
        }
    }
};

// ======================
// User Detail
// ======================

const UserDetail = {
    init() {
        // Copy to clipboard buttons
        document.querySelectorAll('.btn-copy').forEach(btn => {
            btn.addEventListener('click', async (e) => {
                const text = e.target.closest('.btn-copy').dataset.copy;
                const success = await Utils.copyToClipboard(text);

                if (success) {
                    FlashMessages.show('success', 'Copied to clipboard');
                } else {
                    FlashMessages.show('error', 'Failed to copy to clipboard');
                }
            });
        });

        // Delete button
        const deleteBtn = document.querySelector('.delete-user');
        if (deleteBtn) {
            deleteBtn.addEventListener('click', (e) => {
                UserList.handleDelete(e);
            });
        }

        // Update relative times
        this.updateRelativeTimes();
        setInterval(() => this.updateRelativeTimes(), 60000);
    },

    updateRelativeTimes() {
        document.querySelectorAll('.detail-time-relative').forEach(elem => {
            const timestamp = elem.dataset.timestamp;
            if (timestamp) {
                elem.textContent = `(${Utils.getRelativeTime(timestamp)})`;
            }
        });
    }
};

// ======================
// Initialize on DOM Ready
// ======================

document.addEventListener('DOMContentLoaded', () => {
    FlashMessages.init();
    Modal.init();
});

// Export to global scope
window.Utils = Utils;
window.FlashMessages = FlashMessages;
window.Modal = Modal;
window.UserList = UserList;
window.UserForm = UserForm;
window.UserDetail = UserDetail;
