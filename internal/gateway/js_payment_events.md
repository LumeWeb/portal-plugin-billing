# JavaScript Payment Events Specification

This specification defines standard event names for API-based payment gateways that use client-side JavaScript widgets. All API-based gateways emitting payment events must use these event names to provide a consistent interface for frontend applications.

## Event Convention

- **Target**: Events are dispatched on the `window` object using `CustomEvent`
- **Bubbling**: Events bubble by default (`bubbles: true`)
- **Data Payload**: Only `paymentError` includes data; all other events have no detail (`null`)

## Event Names

| Event Name | Description | Event Detail |
|------------|-------------|--------------|
| `paymentSuccess` | Payment completed successfully | `null` (no data) |
| `paymentCanceled` | User canceled the payment flow | `null` (no data) |
| `paymentCompleted` | Payment flow completed (fires after success/canceled) | `null` (no data) |
| `paymentError` | Payment error occurred | `{ error: string }` |

## React Usage Example

```tsx
import { useEffect } from 'react';

export function PaymentListener() {
  useEffect(() => {
    const handleSuccess = () => {
      console.log('Payment successful');
      // Update UI, show success message, etc.
    };

    const handleCanceled = () => {
      console.log('Payment canceled');
      // Handle cancellation UI
    };

    const handleCompleted = () => {
      console.log('Payment completed');
      // Final cleanup regardless of outcome
    };

    const handleError = (e: Event) => {
      const error = (e as CustomEvent).detail?.error;
      console.error('Payment error:', error);
      // Show error message to user
    };

    window.addEventListener('paymentSuccess', handleSuccess);
    window.addEventListener('paymentCanceled', handleCanceled);
    window.addEventListener('paymentCompleted', handleCompleted);
    window.addEventListener('paymentError', handleError);

    return () => {
      window.removeEventListener('paymentSuccess', handleSuccess);
      window.removeEventListener('paymentCanceled', handleCanceled);
      window.removeEventListener('paymentCompleted', handleCompleted);
      window.removeEventListener('paymentError', handleError);
    };
  }, []);

  return null;
}
```

## Implementation Guidelines for Gateway Templates

When implementing a client-side payment widget template:

1. **No redirects**: Do not use `window.location.href` in your templates
2. **Use generic names**: Use the four event names defined in this spec
3. **Error events only**: Only `paymentError` passes data; others call with `null`
4. **Log appropriately**: Include console logs for debugging, but use generic event names

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

## Gateway-Specific Notes

### ATLOS
- Template: `internal/gateway/atlos/templates/payment_button.tpl`
- Widget: Loads `atlos.js` SDK
- Event dispatch: In button click handler

### PayPal (Future)
- Will follow same event spec
- Template location TBD

### Other Gateways
- Any new API-based gateway must implement this spec
- Refer to this document for event names and conventions
