# JavaScript Payment Events Specification

This specification defines standard event names for API-based payment gateways that use client-side JavaScript widgets. All API-based gateways emitting payment events must use these event names to provide a consistent interface for frontend applications.

## Event Convention

- **Target**: Events are dispatched on the `window` object using `CustomEvent`
- **Bubbling**: Events bubble by default (`bubbles: true`)
- **Data Payload**: Only `paymentError` and `paymentCleanupRegister` include data; other events have no detail (`null`)

## Event Names

| Event Name | Description | Event Detail |
|------------|-------------|--------------|
| `paymentSuccess` | Payment completed successfully | `null` (no data) |
| `paymentCanceled` | User canceled the payment flow | `null` (no data) |
| `paymentCompleted` | Payment flow completed (fires after success/canceled) | `null` (no data) |
| `paymentError` | Payment error occurred | `{ error: string }` |
| `paymentCleanupRegister` | Register a cleanup function for gateway resources | `{ cleanup: () => void }` |

## Cleanup Mechanism

Gateway templates that load external scripts or create global references **must** register cleanup functions via the `paymentCleanupRegister` event. This allows the frontend to release resources (remove loaded scripts, unset global variables, destroy SDK instances) when the payment flow completes or the checkout UI is unmounted.

### How It Works

1. **Gateway template dispatches** `paymentCleanupRegister` with a cleanup callback
2. **Frontend listener stores** the callback for later invocation
3. **Frontend invokes cleanup** on `paymentCompleted` or component unmount

### Gateway Template Example

```javascript
// Register cleanup for loaded SDK scripts and global references
(function() {
  var cleanup = function() {
    // Remove all script elements from this gateway's domain
    var scripts = document.querySelectorAll('script[src*="js.stripe.com"]');
    scripts.forEach(function(s) { s.remove(); });
    // Unset global SDK reference
    delete globalThis.Stripe;
  };

  window.dispatchEvent(new CustomEvent('paymentCleanupRegister', {
    detail: { cleanup: cleanup },
    bubbles: true
  }));
})();
```

### Frontend Consumer Example

```javascript
var cleanupFunctions = [];

window.addEventListener('paymentCleanupRegister', function(e) {
  if (typeof e.detail.cleanup === 'function') {
    cleanupFunctions.push(e.detail.cleanup);
  }
});

function runCleanup() {
  cleanupFunctions.forEach(function(fn) { fn(); });
  cleanupFunctions = [];
}

// Call cleanup when payment flow completes
window.addEventListener('paymentCompleted', function() {
  runCleanup();
});

// Also call cleanup on component unmount (React example)
// return () => { runCleanup(); };
```

## React Usage Example

```tsx
import { useEffect, useRef } from 'react';

export function PaymentListener() {
  const cleanupFunctions = useRef<(() => void)[]>([]);

  useEffect(() => {
    const handleSuccess = () => {
      console.log('Payment successful');
    };

    const handleCanceled = () => {
      console.log('Payment canceled');
    };

    const handleCompleted = () => {
      console.log('Payment completed');
      // Run all registered cleanup functions
      cleanupFunctions.current.forEach(fn => fn());
      cleanupFunctions.current = [];
    };

    const handleError = (e: Event) => {
      const error = (e as CustomEvent).detail?.error;
      console.error('Payment error:', error);
    };

    const handleCleanupRegister = (e: Event) => {
      const cleanup = (e as CustomEvent).detail?.cleanup;
      if (typeof cleanup === 'function') {
        cleanupFunctions.current.push(cleanup);
      }
    };

    window.addEventListener('paymentSuccess', handleSuccess);
    window.addEventListener('paymentCanceled', handleCanceled);
    window.addEventListener('paymentCompleted', handleCompleted);
    window.addEventListener('paymentError', handleError);
    window.addEventListener('paymentCleanupRegister', handleCleanupRegister);

    return () => {
      window.removeEventListener('paymentSuccess', handleSuccess);
      window.removeEventListener('paymentCanceled', handleCanceled);
      window.removeEventListener('paymentCompleted', handleCompleted);
      window.removeEventListener('paymentError', handleError);
      window.removeEventListener('paymentCleanupRegister', handleCleanupRegister);
      // Run cleanup on unmount
      cleanupFunctions.current.forEach(fn => fn());
      cleanupFunctions.current = [];
    };
  }, []);

  return null;
}
```

## Implementation Guidelines for Gateway Templates

When implementing a client-side payment widget template:

1. **No redirects**: Do not use `window.location.href` in your templates
2. **Use generic names**: Use the event names defined in this spec
3. **Error events only**: Only `paymentError` passes data; others call with `null` (except `paymentCleanupRegister`)
4. **Log appropriately**: Include console logs for debugging, but use generic event names
5. **Register cleanup**: All templates that load external scripts or set global references must dispatch `paymentCleanupRegister` with a cleanup function

### Example Template Handler

```javascript
function dispatchPaymentEvent(eventName, detail) {
  var event = new CustomEvent(eventName, {
    detail: detail,
    bubbles: true
  });
  window.dispatchEvent(event);
  console.log('Payment event dispatched:', eventName, detail);
}

// Usage
onSuccess: function(response) {
  dispatchPaymentEvent('paymentSuccess', null);
}

onCanceled: function(response) {
  dispatchPaymentEvent('paymentCanceled', null);
}

onCompleted: function(response) {
  dispatchPaymentEvent('paymentCompleted', null);
}

onError: function(error) {
  dispatchPaymentEvent('paymentError', { error: error.message || error });
}
```

### Cleanup Registration Pattern

```javascript
// Called immediately after loading the gateway SDK
dispatchPaymentEvent('paymentCleanupRegister', {
  cleanup: function() {
    // Remove gateway script elements
    var scripts = document.querySelectorAll('script[src*="cdn.example.com"]');
    scripts.forEach(function(s) { s.remove(); });
    // Remove global references
    delete globalThis.ExampleSDK;
  }
});
```

## Gateway-Specific Notes

### Stripe
- Template: `internal/gateway/stripe/templates/embedded_checkout.tpl`
- SDK: Loads `https://js.stripe.com/dahlia/stripe.js`
- Global: Creates `globalThis.Stripe(...)`
- Cleanup: Removes `js.stripe.com` script elements, unsets `globalThis.Stripe`
- Event dispatch: In embedded checkout initialization

### ATLOS
- Template: `internal/gateway/atlos/templates/payment_button.tpl`
- SDK: Loads `https://atlos.io/packages/app/atlos.js`
- Global: Uses `window.atlos.Pay(...)`
- Cleanup: Removes `atlos.io/packages` script elements, unsets `globalThis.atlos`, removes `atlos-modal` and `w3m-modal` custom elements
- Event dispatch: In button click handler

### Other Gateways
- Any new API-based gateway must implement this spec including cleanup registration
- Refer to this document for event names and conventions
