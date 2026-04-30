(function() {
	var stripe = globalThis.Stripe('{{.PublishableKey}}');
	var checkoutInstance = null;
	var cleanedUp = false;

	async function initialize() {
		checkoutInstance = await stripe.createEmbeddedCheckoutPage({
			fetchClientSecret: async () => '{{.ClientSecret}}'
		});

		// Register onComplete callback to signal client-side completion
		// Per js_payment_events.md spec: paymentCompleted has detail: null
		// sessionId must come from the usePaymentEvents sessionId prop, NOT from event detail
		checkoutInstance.on('complete', function() {
			window.dispatchEvent(new CustomEvent('paymentCompleted', {
				bubbles: true
			}));
		});

		if (!cleanedUp) {
			checkoutInstance.mount('#stripe-checkout');
		}
	}

	window.dispatchEvent(new CustomEvent('paymentCleanupRegister', {
		detail: {
			cleanup: function() {
				cleanedUp = true;
				if (checkoutInstance) {
					checkoutInstance.unmount();
				}
				var container = document.getElementById('stripe-checkout');
				if (container) { container.innerHTML = ''; }
				var scripts = document.querySelectorAll('script[src*="js.stripe.com"]');
				scripts.forEach(function(s) { s.remove(); });
				delete globalThis.Stripe;
			}
		},
		bubbles: true
	}));

	initialize();
})();
