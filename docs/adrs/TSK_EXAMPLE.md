# Task: Front-End Client Implementation for go-router Example

## Overview
This document outlines the complete implementation plan for adding a front-end client to the `examples/router` demo application. The front-end will provide a modern, interactive UI for demonstrating CRUD operations on the User resource using the existing go-router API.

## Project Structure

```
examples/router/
├── main.go                          # Backend server (to be updated)
├── main_test.go                     # Tests
└── views/
    ├── layout.html                  # Base layout template (new)
    ├── index.html                   # User list page (update)
    ├── user-form.html              # Create/Edit form (new)
    ├── user-detail.html            # User detail view (new)
    ├── error.html                  # Error page (new)
    ├── css/
    │   └── main.css                # Styles (new)
    └── js/
        └── app.js                  # Client-side JavaScript (new)
```

## Detailed Task Breakdown

### Phase 1: View Templates

#### Task 1.1: Create Base Layout Template
**File**: `examples/router/views/layout.html`

**Requirements**:
- Define HTML5 structure with responsive meta tags
- Include navigation bar with links to home and create user pages
- Add flash message container for success/error notifications
- Link to CSS and JavaScript files
- Define content block for child templates
- Add footer with project information

**Key Features**:
- Responsive design using modern CSS
- Flash message display with auto-dismiss functionality
- Clean, professional navigation
- SEO-friendly markup

---

#### Task 1.2: Update Index Page
**File**: `examples/router/views/index.html` (update existing)

**Requirements**:
- Extend base layout template
- Display user list in a responsive table/card layout
- Add search/filter functionality
- Include action buttons (view, edit, delete) for each user
- Show "Create New User" call-to-action button
- Handle empty state when no users exist
- Display user count and metadata

**Key Features**:
- Real-time search without page reload
- Sortable columns (name, email, created date)
- Responsive cards for mobile view
- Confirmation dialog for delete operations
- Loading states during API calls

---

#### Task 1.3: Create User Form Template
**File**: `examples/router/views/user-form.html`

**Requirements**:
- Reusable form for both create and edit operations
- Form fields: Name (required), Email (required, validated)
- Client-side validation before submission
- Server-side error display from validation errors
- Cancel button to return to list
- Different title/submit button text for create vs edit

**Key Features**:
- HTML5 form validation
- Real-time field validation feedback
- Accessible form labels and error messages
- Loading state during form submission
- CSRF protection ready

---

#### Task 1.4: Create User Detail Template
**File**: `examples/router/views/user-detail.html`

**Requirements**:
- Display all user information in a clean layout
- Show metadata (ID, created date, updated date)
- Include Edit and Delete action buttons
- Back to list navigation
- Format dates in readable format

**Key Features**:
- Clean, card-based layout
- Prominent action buttons
- Formatted timestamps
- Breadcrumb navigation

---

#### Task 1.5: Create Error Page Template
**File**: `examples/router/views/error.html`

**Requirements**:
- Display error message and code
- Friendly error messages for common errors (404, 500, etc.)
- Back to home button
- Optional error details for development mode

**Key Features**:
- User-friendly error messages
- Appropriate HTTP status codes
- Navigation back to safety
- Development mode stack trace display

---

### Phase 2: Static Assets

#### Task 2.1: Create Main Stylesheet
**File**: `examples/router/views/css/main.css`

**Requirements**:
- Modern, clean design system
- Responsive layout (mobile-first approach)
- CSS variables for theming (colors, spacing, typography)
- Component styles:
  - Navigation bar
  - Tables and cards
  - Forms and inputs
  - Buttons (primary, secondary, danger)
  - Flash messages (success, error, warning, info)
  - Modal dialogs
  - Loading spinners

**Design Principles**:
- Consistent spacing using 8px grid
- Accessible color contrast ratios (WCAG AA)
- Smooth transitions and animations
- No external CSS framework dependencies
- Print-friendly styles

---

#### Task 2.2: Create JavaScript Application
**File**: `examples/router/views/js/app.js`

**Requirements**:
- User list functionality:
  - Real-time search/filter
  - AJAX-based delete with confirmation
  - Sort by column
- Form validation:
  - Email format validation
  - Required field checks
  - Display inline error messages
- Flash message handling:
  - Auto-dismiss after 5 seconds
  - Manual dismiss on click
- API client:
  - Fetch wrapper for API calls
  - Error handling and display
  - Loading state management
- Utility functions:
  - Date formatting
  - Debounce for search
  - Confirmation dialogs

**Technical Requirements**:
- Pure JavaScript (no jQuery or frameworks)
- ES6+ syntax
- Async/await for API calls
- Event delegation for dynamic content
- Graceful degradation if JavaScript disabled

---

### Phase 3: Backend Route Handlers

#### Task 3.1: Add Front-End Route Group
**File**: `examples/router/main.go`

**Location**: After API routes, before server start

**New Routes**:
```go
// Front-end routes (HTML rendering)
front.Get("/", renderUserList)              // User list page
front.Get("/users/new", renderCreateForm)    // Create user form
front.Get("/users/:id", renderUserDetail)    // User detail page
front.Get("/users/:id/edit", renderEditForm) // Edit user form
front.Post("/users", handleCreateUser)       // Form submission (create)
front.Post("/users/:id", handleUpdateUser)   // Form submission (update)
front.Post("/users/:id/delete", handleDeleteUser) // Delete with redirect
```

---

#### Task 3.2: Implement Handler Functions

**Handler**: `renderUserList(c router.Context) error`
- Fetch all users from store
- Pass users to index.html template
- Handle empty state
- Render with layout

**Handler**: `renderCreateForm(c router.Context) error`
- Render user-form.html with mode="create"
- Empty form fields

**Handler**: `renderUserDetail(c router.Context) error`
- Get user ID from params
- Fetch user from store
- Return 404 if not found
- Render user-detail.html

**Handler**: `renderEditForm(c router.Context) error`
- Get user ID from params
- Fetch user from store
- Pre-populate form with user data
- Render user-form.html with mode="edit"

**Handler**: `handleCreateUser(c router.Context) error`
- Parse form data
- Validate input
- Create user in store
- Set flash success message
- Redirect to user detail page

**Handler**: `handleUpdateUser(c router.Context) error`
- Get user ID from params
- Parse form data
- Validate input
- Update user in store
- Set flash success message
- Redirect to user detail page

**Handler**: `handleDeleteUser(c router.Context) error`
- Get user ID from params
- Delete user from store
- Set flash success message
- Redirect to user list

---

### Phase 4: Middleware & Configuration

#### Task 4.1: Add Flash Middleware
**Location**: Before front-end routes in `main.go`

**Requirements**:
- Import flash middleware from go-router
- Configure with default settings
- Ensure cookies are properly set

```go
import "github.com/goliatone/go-router/middleware/flash"

front.Use(router.ToMiddleware(flash.New()))
```

---

#### Task 4.2: Add Route Context Middleware
**Location**: Before front-end routes in `main.go`

**Requirements**:
- Import routecontext middleware
- Enable template access to route information
- Configure for map export mode

```go
import "github.com/goliatone/go-router/middleware/routecontext"

front.Use(router.ToMiddleware(routecontext.New()))
```

---

#### Task 4.3: Configure Static File Serving
**Location**: After Fiber app initialization in `main.go`

**Requirements**:
- Serve static files from views/css and views/js
- Configure proper cache headers
- Use Fiber's static middleware

```go
app.WrappedRouter().Static("/static", "./views")
```

---

#### Task 4.4: Update Template Engine Configuration
**Location**: In `newFiberAdapter()` function

**Current**:
```go
engine := django.New("./views", ".html")
```

**Verify**:
- Template directory is correct
- File extension matches
- Reload option for development

---

### Phase 5: Features Implementation

#### Feature 5.1: User List with Search
**Components**: `index.html`, `app.js`, `main.css`

**Implementation**:
- Add search input in page header
- JavaScript debounced search function
- Filter users by name or email
- Update table/cards without page reload
- Show "no results" message when filtered

---

#### Feature 5.2: Form Validation
**Components**: `user-form.html`, `app.js`, `main.css`

**Client-Side**:
- Required field validation
- Email format validation
- Real-time feedback on blur
- Prevent submission if invalid

**Server-Side**:
- Validate in handler functions
- Return validation errors to template
- Display errors inline on form

---

#### Feature 5.3: Delete Confirmation
**Components**: `index.html`, `app.js`, `main.css`

**Implementation**:
- Modal confirmation dialog
- Display user name in confirmation
- AJAX delete request
- Update UI on success
- Show error message on failure

---

#### Feature 5.4: Flash Messages
**Components**: `layout.html`, `app.js`, `main.css`

**Implementation**:
- Auto-dismiss after 5 seconds
- Manual close button
- Slide-in animation
- Different styles for success/error/warning/info
- Stack multiple messages

---

#### Feature 5.5: Loading States
**Components**: All templates, `app.js`, `main.css`

**Implementation**:
- Disable buttons during submission
- Show spinner on buttons
- Overlay for page-level loading
- Prevent double submission

---

#### Feature 5.6: Responsive Design
**Components**: All templates, `main.css`

**Breakpoints**:
- Mobile: < 640px (cards, stacked forms)
- Tablet: 640px - 1024px (hybrid layout)
- Desktop: > 1024px (full table layout)

**Mobile Optimizations**:
- Hamburger menu for navigation
- Cards instead of tables
- Touch-friendly buttons (44px min)
- Simplified forms

---

### Phase 6: Testing & Refinement

#### Task 6.1: Manual Testing Checklist
- [ ] User list loads and displays all users
- [ ] Search filters users correctly
- [ ] Create user form validates and submits
- [ ] Edit user form pre-populates and updates
- [ ] Delete confirmation works and removes user
- [ ] Flash messages display and auto-dismiss
- [ ] All links navigate correctly
- [ ] 404 page shows for invalid user IDs
- [ ] Responsive design works on mobile
- [ ] Browser back/forward buttons work
- [ ] Form validation prevents invalid submissions

---

#### Task 6.2: Browser Compatibility
**Target Browsers**:
- Chrome (latest)
- Firefox (latest)
- Safari (latest)
- Edge (latest)

**Features to Test**:
- CSS Grid and Flexbox
- Fetch API
- ES6+ JavaScript
- HTML5 form validation

---

#### Task 6.3: Accessibility Audit
- [ ] Semantic HTML elements
- [ ] Proper heading hierarchy (h1-h6)
- [ ] Alt text for images (if any)
- [ ] Form labels associated with inputs
- [ ] ARIA labels where needed
- [ ] Keyboard navigation works
- [ ] Focus indicators visible
- [ ] Color contrast meets WCAG AA

---

## Implementation Order

1. **Start**: Create TSK_EXAMPLE.md (this document)
2. **Templates**: Create all view templates (layout, index, forms, detail, error)
3. **Styles**: Create main.css with complete component library
4. **JavaScript**: Create app.js with all client-side functionality
5. **Backend**: Update main.go with route handlers and middleware
6. **Testing**: Manual testing and refinement
7. **Documentation**: Update main README with example usage

## Development Tips

### Running the Example
```bash
cd examples/router
go run main.go
# Visit http://localhost:9092
```

### Live Reload for Templates
Set `Reload: true` in template engine config during development.

### API Endpoints
The existing API remains at `/api/users/*` and can be used by the JavaScript for AJAX operations.

### Template Syntax
Using Django/Pongo2 template syntax:
- Variables: `{{ variable }}`
- Conditions: `{% if condition %}...{% endif %}`
- Loops: `{% for item in items %}...{% endfor %}`
- Blocks: `{% block content %}...{% endblock %}`

## Success Criteria

- ✅ User can view list of all users
- ✅ User can search/filter users
- ✅ User can create new user via form
- ✅ User can edit existing user via form
- ✅ User can delete user with confirmation
- ✅ All CRUD operations show success/error messages
- ✅ UI is responsive and works on mobile
- ✅ No JavaScript errors in console
- ✅ Forms validate input properly
- ✅ Navigation is intuitive and works correctly

## Future Enhancements

- Pagination for user list
- Sorting by different columns
- Advanced filters (date range, etc.)
- Bulk operations (delete multiple users)
- User profile pictures
- Export user list to CSV
- Import users from file
- User activity log
- WebSocket live updates when users change
