(function() {
	var stripe = globalThis.Stripe('{{.PublishableKey}}');
	var checkoutInstance = null;
	var cleanedUp = false;

	async function initialize() {
		checkoutInstance = await stripe.createEmbeddedCheckoutPage({
			clientSecret: '{{.ClientSecret}}',
			onComplete: function() {
				window.dispatchEvent(new CustomEvent('paymentCompleted', {
					bubbles: true
				}));
			}
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
